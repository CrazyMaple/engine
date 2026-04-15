package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// --- GM 管理后台框架 ---

// GMRole GM 用户角色
type GMRole string

const (
	GMRoleAdmin    GMRole = "admin"    // 管理员 — 全部权限
	GMRoleOperator GMRole = "operator" // 运营 — 发放/邮件/公告等
	GMRoleCS       GMRole = "cs"       // 客服 — 查询/踢人等
	GMRoleReadOnly GMRole = "readonly" // 只读 — 仅查看
)

// gmRoleLevel 角色权限级别（数值越大权限越高）
var gmRoleLevel = map[GMRole]int{
	GMRoleReadOnly: 0,
	GMRoleCS:       1,
	GMRoleOperator: 2,
	GMRoleAdmin:    3,
}

// GMUser GM 系统用户
type GMUser struct {
	Username string `json:"username"`
	Role     GMRole `json:"role"`
	Token    string `json:"-"` // 不序列化到 JSON 响应
}

// GMCommand GM 命令定义
type GMCommand struct {
	Name        string   `json:"name"`         // 命令名称，如 "set_attr"
	Description string   `json:"description"`  // 命令说明
	MinRole     GMRole   `json:"min_role"`      // 最低权限要求
	Args        []GMArg  `json:"args"`          // 参数列表
	BatchOK     bool     `json:"batch_ok"`      // 是否支持批量操作
	Handler     GMHandler `json:"-"`            // 执行函数
}

// GMArg GM 命令参数定义
type GMArg struct {
	Name     string `json:"name"`
	Type     string `json:"type"`     // "string", "int", "bool", "[]string"
	Required bool   `json:"required"`
	Desc     string `json:"desc"`
}

// GMHandler GM 命令执行函数签名
// ctx 包含操作人信息，args 为命令参数，返回执行结果
type GMHandler func(ctx *GMContext, args map[string]interface{}) (interface{}, error)

// GMContext GM 命令执行上下文
type GMContext struct {
	Operator string    // 操作人
	Role     GMRole    // 操作人角色
	SourceIP string    // 来源 IP
	Time     time.Time // 操作时间
}

// GMResult GM 命令执行结果
type GMResult struct {
	Command  string      `json:"command"`
	Success  bool        `json:"success"`
	Result   interface{} `json:"result,omitempty"`
	Error    string      `json:"error,omitempty"`
	Operator string      `json:"operator"`
	Time     time.Time   `json:"time"`
}

// GMManager GM 管理器
type GMManager struct {
	mu       sync.RWMutex
	commands map[string]*GMCommand
	users    map[string]*GMUser // token → user
	audit    *AuditLog
}

// NewGMManager 创建 GM 管理器
func NewGMManager(audit *AuditLog) *GMManager {
	if audit == nil {
		audit = NewAuditLog()
	}
	return &GMManager{
		commands: make(map[string]*GMCommand),
		users:    make(map[string]*GMUser),
		audit:    audit,
	}
}

// RegisterCommand 注册 GM 命令
func (gm *GMManager) RegisterCommand(cmd *GMCommand) {
	gm.mu.Lock()
	gm.commands[cmd.Name] = cmd
	gm.mu.Unlock()
}

// RegisterUser 注册 GM 用户
func (gm *GMManager) RegisterUser(username string, role GMRole, token string) {
	gm.mu.Lock()
	gm.users[token] = &GMUser{
		Username: username,
		Role:     role,
		Token:    token,
	}
	gm.mu.Unlock()
}

// RemoveUser 移除 GM 用户
func (gm *GMManager) RemoveUser(token string) {
	gm.mu.Lock()
	delete(gm.users, token)
	gm.mu.Unlock()
}

// Authenticate 根据 token 认证用户
func (gm *GMManager) Authenticate(r *http.Request) (*GMUser, bool) {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	// 优先从 X-GM-Token header 获取
	token := r.Header.Get("X-GM-Token")
	if token == "" {
		// 降级到 Bearer Token
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		}
	}

	user, ok := gm.users[token]
	return user, ok
}

// CheckPermission 检查用户是否有执行该命令的权限
func (gm *GMManager) CheckPermission(user *GMUser, cmd *GMCommand) bool {
	return gmRoleLevel[user.Role] >= gmRoleLevel[cmd.MinRole]
}

// Execute 执行 GM 命令
func (gm *GMManager) Execute(user *GMUser, cmdName string, args map[string]interface{}, sourceIP string) *GMResult {
	gm.mu.RLock()
	cmd, exists := gm.commands[cmdName]
	gm.mu.RUnlock()

	result := &GMResult{
		Command:  cmdName,
		Operator: user.Username,
		Time:     time.Now(),
	}

	if !exists {
		result.Error = fmt.Sprintf("unknown command: %s", cmdName)
		return result
	}

	if !gm.CheckPermission(user, cmd) {
		result.Error = fmt.Sprintf("permission denied: %s requires %s, user has %s", cmdName, cmd.MinRole, user.Role)
		gm.audit.Record("gm_denied", fmt.Sprintf("%s: %v", cmdName, args), "gm", user.Username, sourceIP)
		return result
	}

	// 验证必填参数
	for _, arg := range cmd.Args {
		if arg.Required {
			if _, ok := args[arg.Name]; !ok {
				result.Error = fmt.Sprintf("missing required argument: %s", arg.Name)
				return result
			}
		}
	}

	ctx := &GMContext{
		Operator: user.Username,
		Role:     user.Role,
		SourceIP: sourceIP,
		Time:     time.Now(),
	}

	out, err := cmd.Handler(ctx, args)
	if err != nil {
		result.Error = err.Error()
		gm.audit.Record("gm_error", fmt.Sprintf("%s: %v → %v", cmdName, args, err), "gm", user.Username, sourceIP)
	} else {
		result.Success = true
		result.Result = out
		gm.audit.Record("gm_exec", fmt.Sprintf("%s: %v", cmdName, args), "gm", user.Username, sourceIP)
	}

	return result
}

// ExecuteBatch 批量执行 GM 命令
func (gm *GMManager) ExecuteBatch(user *GMUser, cmdName string, batchArgs []map[string]interface{}, sourceIP string) []*GMResult {
	gm.mu.RLock()
	cmd, exists := gm.commands[cmdName]
	gm.mu.RUnlock()

	if !exists {
		return []*GMResult{{Command: cmdName, Error: "unknown command", Operator: user.Username, Time: time.Now()}}
	}
	if !cmd.BatchOK {
		return []*GMResult{{Command: cmdName, Error: "command does not support batch", Operator: user.Username, Time: time.Now()}}
	}

	results := make([]*GMResult, 0, len(batchArgs))
	for _, args := range batchArgs {
		results = append(results, gm.Execute(user, cmdName, args, sourceIP))
	}

	gm.audit.Record("gm_batch", fmt.Sprintf("%s: %d items", cmdName, len(batchArgs)), "gm", user.Username, sourceIP)
	return results
}

// ListCommands 列出所有可用命令（按用户权限过滤）
func (gm *GMManager) ListCommands(user *GMUser) []*GMCommand {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	cmds := make([]*GMCommand, 0)
	for _, cmd := range gm.commands {
		if gm.CheckPermission(user, cmd) {
			cmds = append(cmds, cmd)
		}
	}
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
	return cmds
}

// --- Dashboard REST API 集成 ---

// GMHandlers GM REST API 处理器
type GMHandlers struct {
	gm *GMManager
}

// NewGMHandlers 创建 GM API 处理器
func NewGMHandlers(gm *GMManager) *GMHandlers {
	return &GMHandlers{gm: gm}
}

// RegisterRoutes 将 GM API 注册到 HTTP 路由
func (h *GMHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/gm/commands", h.handleListCommands)
	mux.HandleFunc("/api/gm/execute", h.handleExecute)
	mux.HandleFunc("/api/gm/batch", h.handleBatch)
	mux.HandleFunc("/api/gm/users", h.handleUsers)
}

func (h *GMHandlers) handleListCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user, ok := h.gm.Authenticate(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	cmds := h.gm.ListCommands(user)
	writeJSON(w, cmds)
}

func (h *GMHandlers) handleExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user, ok := h.gm.Authenticate(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Command string                 `json:"command"`
		Args    map[string]interface{} `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	result := h.gm.Execute(user, req.Command, req.Args, r.RemoteAddr)
	writeJSON(w, result)
}

func (h *GMHandlers) handleBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user, ok := h.gm.Authenticate(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Command string                   `json:"command"`
		Batch   []map[string]interface{} `json:"batch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	results := h.gm.ExecuteBatch(user, req.Command, req.Batch, r.RemoteAddr)
	writeJSON(w, results)
}

func (h *GMHandlers) handleUsers(w http.ResponseWriter, r *http.Request) {
	user, ok := h.gm.Authenticate(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if user.Role != GMRoleAdmin {
		http.Error(w, "admin only", http.StatusForbidden)
		return
	}

	h.gm.mu.RLock()
	users := make([]GMUser, 0, len(h.gm.users))
	for _, u := range h.gm.users {
		users = append(users, *u)
	}
	h.gm.mu.RUnlock()

	writeJSON(w, users)
}

// --- 预置 GM 命令注册 ---

// RegisterBuiltinCommands 注册引擎内置 GM 命令
func RegisterBuiltinCommands(gm *GMManager) {
	// 踢人
	gm.RegisterCommand(&GMCommand{
		Name:        "kick_player",
		Description: "踢出指定玩家",
		MinRole:     GMRoleCS,
		Args: []GMArg{
			{Name: "player_id", Type: "string", Required: true, Desc: "玩家 ID"},
			{Name: "reason", Type: "string", Required: false, Desc: "踢出原因"},
		},
		Handler: func(ctx *GMContext, args map[string]interface{}) (interface{}, error) {
			// 占位实现 — 由业务层注入真实逻辑
			return map[string]string{"status": "kicked", "player_id": fmt.Sprintf("%v", args["player_id"])}, nil
		},
	})

	// 封禁/解封
	gm.RegisterCommand(&GMCommand{
		Name:        "ban_player",
		Description: "封禁/解封玩家",
		MinRole:     GMRoleCS,
		BatchOK:     true,
		Args: []GMArg{
			{Name: "player_id", Type: "string", Required: true, Desc: "玩家 ID"},
			{Name: "ban", Type: "bool", Required: true, Desc: "true=封禁, false=解封"},
			{Name: "duration", Type: "string", Required: false, Desc: "封禁时长，如 '24h'（默认永久）"},
			{Name: "reason", Type: "string", Required: false, Desc: "原因"},
		},
		Handler: func(ctx *GMContext, args map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"status": "ok", "player_id": args["player_id"], "ban": args["ban"]}, nil
		},
	})

	// 修改玩家属性
	gm.RegisterCommand(&GMCommand{
		Name:        "set_attr",
		Description: "修改玩家属性（如等级、金币等）",
		MinRole:     GMRoleOperator,
		Args: []GMArg{
			{Name: "player_id", Type: "string", Required: true, Desc: "玩家 ID"},
			{Name: "attr", Type: "string", Required: true, Desc: "属性名"},
			{Name: "value", Type: "string", Required: true, Desc: "属性值"},
		},
		Handler: func(ctx *GMContext, args map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"status": "ok", "player_id": args["player_id"], "attr": args["attr"], "value": args["value"]}, nil
		},
	})

	// 发放道具
	gm.RegisterCommand(&GMCommand{
		Name:        "grant_item",
		Description: "发放道具到玩家背包",
		MinRole:     GMRoleOperator,
		BatchOK:     true,
		Args: []GMArg{
			{Name: "player_id", Type: "string", Required: true, Desc: "玩家 ID"},
			{Name: "item_id", Type: "string", Required: true, Desc: "道具 ID"},
			{Name: "count", Type: "int", Required: true, Desc: "数量"},
		},
		Handler: func(ctx *GMContext, args map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"status": "ok", "player_id": args["player_id"], "item_id": args["item_id"], "count": args["count"]}, nil
		},
	})

	// 发送系统邮件
	gm.RegisterCommand(&GMCommand{
		Name:        "send_mail",
		Description: "发送系统邮件给指定玩家或全服",
		MinRole:     GMRoleOperator,
		BatchOK:     true,
		Args: []GMArg{
			{Name: "player_id", Type: "string", Required: false, Desc: "玩家 ID（空则全服）"},
			{Name: "title", Type: "string", Required: true, Desc: "邮件标题"},
			{Name: "content", Type: "string", Required: true, Desc: "邮件内容"},
			{Name: "attachments", Type: "string", Required: false, Desc: "附件道具（JSON格式）"},
		},
		Handler: func(ctx *GMContext, args map[string]interface{}) (interface{}, error) {
			return map[string]string{"status": "sent"}, nil
		},
	})

	// 发布公告
	gm.RegisterCommand(&GMCommand{
		Name:        "announce",
		Description: "发布全服公告",
		MinRole:     GMRoleOperator,
		Args: []GMArg{
			{Name: "content", Type: "string", Required: true, Desc: "公告内容"},
			{Name: "duration", Type: "string", Required: false, Desc: "展示时长，如 '1h'"},
		},
		Handler: func(ctx *GMContext, args map[string]interface{}) (interface{}, error) {
			return map[string]string{"status": "announced"}, nil
		},
	})

	// 服务器信息查询
	gm.RegisterCommand(&GMCommand{
		Name:        "server_info",
		Description: "查询服务器运行状态",
		MinRole:     GMRoleReadOnly,
		Args:        nil,
		Handler: func(ctx *GMContext, args map[string]interface{}) (interface{}, error) {
			return map[string]string{"status": "running"}, nil
		},
	})

	// 查询玩家信息
	gm.RegisterCommand(&GMCommand{
		Name:        "query_player",
		Description: "查询玩家详细信息",
		MinRole:     GMRoleReadOnly,
		Args: []GMArg{
			{Name: "player_id", Type: "string", Required: true, Desc: "玩家 ID"},
		},
		Handler: func(ctx *GMContext, args map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"player_id": args["player_id"], "status": "placeholder"}, nil
		},
	})
}
