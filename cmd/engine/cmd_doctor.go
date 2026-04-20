package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"engine/config"
)

// cmd_doctor.go — engine doctor 自检命令
//
// 运行：`engine doctor [--config engine.yaml] [--format text|json] [--timeout 3s]`
//
// 输出按如下分组打印诊断结果：
//   - Runtime     Go 版本 / GOOS / GOARCH
//   - Config      engine.yaml 文件存在性、Validate 结果、关键字段摘要
//   - Ports       各监听端口可用性（Remote / Gate / Dashboard / Metrics）
//   - Services    Cluster provider 可达性（consul/etcd/k8s，如配置）
//   - Disk        工作目录与日志目录的可用空间
//
// 每个检查项有 ok / warn / fail 三态：
//   - ok   通过
//   - warn 非致命问题（例：端口未配置、provider 未开启）
//   - fail 必须修复才能正常启动
//
// 退出码：0=全部通过或仅 warn，1=存在 fail

// DoctorReport 整份诊断报告
type DoctorReport struct {
	Host      string         `json:"host"`
	Time      string         `json:"time"`
	Sections  []reportGroup  `json:"sections"`
	Summary   reportSummary  `json:"summary"`
}

type reportGroup struct {
	Name  string        `json:"name"`
	Items []reportItem  `json:"items"`
}

type reportItem struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok|warn|fail
	Detail string `json:"detail"`
}

type reportSummary struct {
	OK   int `json:"ok"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
}

const (
	stOK   = "ok"
	stWarn = "warn"
	stFail = "fail"
)

// minGoVersion 运行引擎所需的最低 Go 版本（与 go.mod 一致）
const minGoVersion = "go1.24"

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	cfgPath := fs.String("config", "engine.yaml", "engine.yaml 路径")
	format := fs.String("format", "text", "输出格式：text|json")
	timeout := fs.Duration("timeout", 3*time.Second, "外部服务探测超时")
	fs.Parse(args)

	host, _ := os.Hostname()
	report := &DoctorReport{
		Host: host,
		Time: time.Now().Format(time.RFC3339),
	}

	report.Sections = append(report.Sections, checkRuntime())

	cfg, cfgGroup := checkConfig(*cfgPath)
	report.Sections = append(report.Sections, cfgGroup)

	report.Sections = append(report.Sections, checkPorts(cfg))
	report.Sections = append(report.Sections, checkServices(cfg, *timeout))
	report.Sections = append(report.Sections, checkDisk(cfg))

	for _, g := range report.Sections {
		for _, it := range g.Items {
			switch it.Status {
			case stOK:
				report.Summary.OK++
			case stWarn:
				report.Summary.Warn++
			case stFail:
				report.Summary.Fail++
			}
		}
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	default:
		printText(report)
	}

	if report.Summary.Fail > 0 {
		os.Exit(1)
	}
	return nil
}

// ------------------------------------------------------------
// checks
// ------------------------------------------------------------

func checkRuntime() reportGroup {
	g := reportGroup{Name: "Runtime"}
	v := runtime.Version()
	if versionLess(v, minGoVersion) {
		g.Items = append(g.Items, reportItem{
			Name: "Go version", Status: stFail,
			Detail: fmt.Sprintf("%s < 要求 %s；请升级 Go 工具链", v, minGoVersion),
		})
	} else {
		g.Items = append(g.Items, reportItem{
			Name: "Go version", Status: stOK,
			Detail: v,
		})
	}
	g.Items = append(g.Items, reportItem{
		Name: "GOOS/GOARCH", Status: stOK,
		Detail: fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	})
	g.Items = append(g.Items, reportItem{
		Name: "NumCPU", Status: stOK,
		Detail: strconv.Itoa(runtime.NumCPU()),
	})
	return g
}

func checkConfig(path string) (*config.EngineConfig, reportGroup) {
	g := reportGroup{Name: "Config"}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			g.Items = append(g.Items, reportItem{
				Name: "File", Status: stWarn,
				Detail: fmt.Sprintf("%s 不存在，将使用默认配置进行后续诊断", path),
			})
			cfg := config.DefaultEngineConfig()
			return cfg, g
		}
		g.Items = append(g.Items, reportItem{
			Name: "File", Status: stFail,
			Detail: fmt.Sprintf("stat %s: %v", path, err),
		})
		return config.DefaultEngineConfig(), g
	}
	g.Items = append(g.Items, reportItem{
		Name: "File", Status: stOK, Detail: path,
	})

	cfg, err := config.LoadEngineConfig(path)
	if err != nil {
		g.Items = append(g.Items, reportItem{
			Name: "Validate", Status: stFail, Detail: err.Error(),
		})
		return config.DefaultEngineConfig(), g
	}
	g.Items = append(g.Items, reportItem{
		Name: "Validate", Status: stOK,
		Detail: fmt.Sprintf("node_id=%s version=%s cluster.enabled=%v",
			cfg.NodeID, cfg.Version, cfg.Cluster.Enabled),
	})
	return cfg, g
}

func checkPorts(cfg *config.EngineConfig) reportGroup {
	g := reportGroup{Name: "Ports"}
	endpoints := []struct {
		name  string
		addr  string
		proto string
	}{
		{"remote.address", cfg.Remote.Address, "tcp"},
		{"gate.tcp_addr", cfg.Gate.TCPAddr, "tcp"},
		{"gate.ws_addr", cfg.Gate.WSAddr, "tcp"},
		{"gate.kcp_addr", cfg.Gate.KCPAddr, "udp"},
		{"dashboard.listen", cfg.Dashboard.Listen, "tcp"},
		{"metrics.listen", cfg.Metrics.Listen, "tcp"},
	}
	for _, e := range endpoints {
		if e.addr == "" {
			g.Items = append(g.Items, reportItem{
				Name: e.name, Status: stWarn, Detail: "未配置",
			})
			continue
		}
		if err := probeListen(e.proto, e.addr); err != nil {
			g.Items = append(g.Items, reportItem{
				Name: e.name, Status: stFail,
				Detail: fmt.Sprintf("%s 不可监听：%v", e.addr, err),
			})
			continue
		}
		g.Items = append(g.Items, reportItem{
			Name: e.name, Status: stOK,
			Detail: fmt.Sprintf("%s/%s 可监听", e.addr, e.proto),
		})
	}
	return g
}

func checkServices(cfg *config.EngineConfig, timeout time.Duration) reportGroup {
	g := reportGroup{Name: "Services"}
	if !cfg.Cluster.Enabled {
		g.Items = append(g.Items, reportItem{
			Name: "cluster", Status: stWarn, Detail: "cluster.enabled=false，跳过外部服务探测",
		})
		return g
	}

	provider := strings.ToLower(strings.TrimSpace(cfg.Cluster.Provider))
	switch provider {
	case "", "static":
		g.Items = append(g.Items, reportItem{
			Name: "provider", Status: stOK,
			Detail: fmt.Sprintf("static seeds=%v", cfg.Cluster.Seeds),
		})
		for _, seed := range cfg.Cluster.Seeds {
			status, detail := probeTCP(seed, timeout)
			g.Items = append(g.Items, reportItem{
				Name: "seed " + seed, Status: status, Detail: detail,
			})
		}
	case "consul":
		addr := firstEnv("ENGINE_CONSUL_ADDR", "CONSUL_HTTP_ADDR")
		if addr == "" {
			addr = "127.0.0.1:8500"
		}
		status, detail := probeHTTP("http://"+stripScheme(addr)+"/v1/status/leader", timeout)
		g.Items = append(g.Items, reportItem{
			Name: "consul", Status: status, Detail: addr + " — " + detail,
		})
	case "etcd":
		addr := firstEnv("ENGINE_ETCD_ADDR", "ETCD_ENDPOINTS")
		if addr == "" {
			addr = "127.0.0.1:2379"
		}
		addr = strings.SplitN(addr, ",", 2)[0]
		status, detail := probeHTTP("http://"+stripScheme(addr)+"/version", timeout)
		g.Items = append(g.Items, reportItem{
			Name: "etcd", Status: status, Detail: addr + " — " + detail,
		})
	case "k8s":
		if os.Getenv("KUBERNETES_SERVICE_HOST") == "" {
			g.Items = append(g.Items, reportItem{
				Name: "k8s", Status: stWarn,
				Detail: "未检测到 Kubernetes in-cluster 环境变量（KUBERNETES_SERVICE_HOST）；若本机运行请忽略",
			})
		} else {
			g.Items = append(g.Items, reportItem{
				Name: "k8s", Status: stOK,
				Detail: fmt.Sprintf("in-cluster: %s:%s",
					os.Getenv("KUBERNETES_SERVICE_HOST"),
					os.Getenv("KUBERNETES_SERVICE_PORT")),
			})
		}
	default:
		g.Items = append(g.Items, reportItem{
			Name: "provider", Status: stFail,
			Detail: fmt.Sprintf("未知 provider: %s", provider),
		})
	}

	if mongo := os.Getenv("ENGINE_MONGO_URI"); mongo != "" {
		host := extractMongoHost(mongo)
		status, detail := probeTCP(host, timeout)
		g.Items = append(g.Items, reportItem{
			Name: "mongodb", Status: status, Detail: mongo + " — " + detail,
		})
	}
	return g
}

func checkDisk(cfg *config.EngineConfig) reportGroup {
	g := reportGroup{Name: "Disk"}
	cwd, _ := os.Getwd()
	if free, total, err := diskUsage(cwd); err != nil {
		g.Items = append(g.Items, reportItem{
			Name: "cwd", Status: stWarn, Detail: fmt.Sprintf("%s: %v", cwd, err),
		})
	} else {
		g.Items = append(g.Items, reportItem{
			Name: "cwd", Status: diskStatus(free),
			Detail: fmt.Sprintf("%s: free=%s total=%s", cwd, humanBytes(free), humanBytes(total)),
		})
	}
	if cfg.Log.Path != "" {
		dir := filepath.Dir(cfg.Log.Path)
		if _, err := os.Stat(dir); err != nil {
			g.Items = append(g.Items, reportItem{
				Name: "log.path", Status: stFail,
				Detail: fmt.Sprintf("%s 不存在或不可访问：%v", dir, err),
			})
		} else if free, total, err := diskUsage(dir); err == nil {
			g.Items = append(g.Items, reportItem{
				Name: "log.path", Status: diskStatus(free),
				Detail: fmt.Sprintf("%s: free=%s total=%s", dir, humanBytes(free), humanBytes(total)),
			})
		}
	}
	return g
}

// ------------------------------------------------------------
// helpers
// ------------------------------------------------------------

// versionLess 按词典序粗略比较 Go 版本字符串（goX.Y.Z）
func versionLess(have, min string) bool {
	// go1.24.3 → 1.24.3
	h := strings.TrimPrefix(have, "go")
	m := strings.TrimPrefix(min, "go")
	hp := strings.Split(h, ".")
	mp := strings.Split(m, ".")
	for i := 0; i < len(mp); i++ {
		var hv, mv int
		if i < len(hp) {
			hv, _ = strconv.Atoi(stripNonDigit(hp[i]))
		}
		mv, _ = strconv.Atoi(stripNonDigit(mp[i]))
		if hv < mv {
			return true
		}
		if hv > mv {
			return false
		}
	}
	return false
}

func stripNonDigit(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			b = append(b, s[i])
		}
	}
	return string(b)
}

// probeListen 判断 addr:port 是否可被当前进程监听
func probeListen(proto, addr string) error {
	switch proto {
	case "tcp":
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		return ln.Close()
	case "udp":
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return err
		}
		c, err := net.ListenUDP("udp", udpAddr)
		if err != nil {
			return err
		}
		return c.Close()
	}
	return fmt.Errorf("unknown proto %q", proto)
}

func probeTCP(addr string, timeout time.Duration) (string, string) {
	c, err := net.DialTimeout("tcp", stripScheme(addr), timeout)
	if err != nil {
		return stFail, err.Error()
	}
	_ = c.Close()
	return stOK, "连通"
}

func probeHTTP(url string, timeout time.Duration) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return stFail, err.Error()
	}
	cli := &http.Client{Timeout: timeout}
	resp, err := cli.Do(req)
	if err != nil {
		return stFail, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return stFail, fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return stOK, fmt.Sprintf("HTTP %d", resp.StatusCode)
}

func stripScheme(addr string) string {
	for _, p := range []string{"http://", "https://", "tcp://", "mongodb://", "mongodb+srv://"} {
		if strings.HasPrefix(addr, p) {
			addr = strings.TrimPrefix(addr, p)
		}
	}
	if i := strings.IndexAny(addr, "/?"); i >= 0 {
		addr = addr[:i]
	}
	return addr
}

func extractMongoHost(uri string) string {
	s := stripScheme(uri)
	if i := strings.Index(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.Index(s, ","); i >= 0 {
		s = s[:i]
	}
	if !strings.Contains(s, ":") {
		s += ":27017"
	}
	return s
}

func diskUsage(path string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	// Bavail 为非特权进程可用；Blocks 为总块数
	return uint64(st.Bavail) * uint64(st.Bsize), uint64(st.Blocks) * uint64(st.Bsize), nil
}

// diskStatus 低于 1 GiB 报 warn，低于 128 MiB 报 fail
func diskStatus(free uint64) string {
	const mib = 1024 * 1024
	switch {
	case free < 128*mib:
		return stFail
	case free < 1024*mib:
		return stWarn
	default:
		return stOK
	}
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// ------------------------------------------------------------
// text renderer
// ------------------------------------------------------------

func printText(r *DoctorReport) {
	fmt.Printf("engine doctor — %s @ %s\n\n", r.Host, r.Time)
	for _, g := range r.Sections {
		fmt.Printf("[%s]\n", g.Name)
		for _, it := range g.Items {
			fmt.Printf("  %s %-20s %s\n", icon(it.Status), it.Name, it.Detail)
		}
		fmt.Println()
	}
	fmt.Printf("汇总: ok=%d warn=%d fail=%d\n",
		r.Summary.OK, r.Summary.Warn, r.Summary.Fail)
	if r.Summary.Fail > 0 {
		fmt.Println("存在致命问题，请修复后再启动 engine")
	} else if r.Summary.Warn > 0 {
		fmt.Println("存在告警，可继续启动但建议排查")
	} else {
		fmt.Println("全部检查通过")
	}
}

func icon(status string) string {
	switch status {
	case stOK:
		return "[OK] "
	case stWarn:
		return "[WARN]"
	case stFail:
		return "[FAIL]"
	default:
		return "[??]  "
	}
}
