package dashboard

// dashboardHTML 嵌入式单页 Dashboard HTML
const dashboardHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>MapleWish Engine Dashboard</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#1a1a2e;color:#e0e0e0;padding:20px}
h1{color:#0ff;margin-bottom:20px;font-size:24px}
h2{color:#7fdbff;margin:20px 0 10px;font-size:18px;border-bottom:1px solid #333;padding-bottom:5px}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(400px,1fr));gap:20px;margin-bottom:20px}
.card{background:#16213e;border:1px solid #0f3460;border-radius:8px;padding:16px}
.card h3{color:#0ff;margin-bottom:10px;font-size:16px}
table{width:100%;border-collapse:collapse;font-size:14px}
th{background:#0f3460;color:#7fdbff;text-align:left;padding:8px;font-weight:600}
td{padding:8px;border-bottom:1px solid #1a1a3e}
tr:hover td{background:#1a1a3e}
.status-alive{color:#2ecc71}
.status-suspect{color:#f39c12}
.status-dead{color:#e74c3c}
.status-left{color:#95a5a6}
.metric{display:inline-block;background:#0f3460;padding:8px 16px;border-radius:4px;margin:4px}
.metric .label{font-size:12px;color:#7fdbff}
.metric .value{font-size:20px;font-weight:bold;color:#0ff}
.refresh-btn{background:#0f3460;color:#0ff;border:1px solid #0ff;padding:6px 16px;border-radius:4px;cursor:pointer;font-size:14px}
.refresh-btn:hover{background:#0ff;color:#0f3460}
.toolbar{display:flex;justify-content:space-between;align-items:center;margin-bottom:20px;flex-wrap:wrap;gap:10px}
.toolbar-right{display:flex;align-items:center;gap:12px}
.auto-refresh{color:#7fdbff;font-size:13px}
#error{color:#e74c3c;padding:10px;display:none}
select.level-select{background:#0f3460;color:#0ff;border:1px solid #0ff;padding:4px 8px;border-radius:4px;font-size:13px;cursor:pointer}
canvas{border-radius:8px}
.reload-btn{background:#0f3460;color:#0ff;border:1px solid #0ff;padding:2px 10px;border-radius:3px;cursor:pointer;font-size:12px}
.reload-btn:hover{background:#0ff;color:#0f3460}
</style>
</head>
<body>
<div class="toolbar">
<h1>MapleWish Engine Dashboard</h1>
<div class="toolbar-right">
<label style="color:#7fdbff;font-size:13px">日志级别:
<select class="level-select" id="logLevel" onchange="changeLogLevel(this.value)">
<option value="debug">DEBUG</option>
<option value="info">INFO</option>
<option value="warn">WARN</option>
<option value="error">ERROR</option>
</select>
</label>
<label class="auto-refresh"><input type="checkbox" id="autoRefresh" checked> 自动刷新 (5s)</label>
<button class="refresh-btn" onclick="refreshAll()">刷新</button>
</div>
</div>
<div id="error"></div>

<div class="grid">
<div class="card" id="systemCard"><h3>系统概览</h3><div id="systemInfo">加载中...</div></div>
<div class="card" id="clusterCard"><h3>集群状态</h3><div id="clusterInfo">加载中...</div></div>
</div>

<h2>运行时指标</h2>
<div class="card"><div id="runtimeInfo">加载中...</div></div>

<h2>消息流量趋势 (最近5分钟)</h2>
<div class="card"><canvas id="trafficChart" width="900" height="250"></canvas></div>

<div class="grid">
<div>
<h2>集群拓扑图</h2>
<div class="card"><canvas id="clusterGraph" width="420" height="300"></canvas></div>
</div>
<div>
<h2>Actor 火焰图</h2>
<div class="card"><canvas id="flameGraph" width="420" height="300"></canvas></div>
</div>
</div>

<h2>Actor 拓扑</h2>
<div class="card"><div id="actorTopology">加载中...</div></div>

<h2>Actor 列表</h2>
<div class="card"><div id="actorList">加载中...</div></div>

<h2>热点 Actor (Top 20)</h2>
<div class="card"><div id="hotActors">加载中...</div></div>

<h2>消息指标</h2>
<div class="card"><div id="metricsInfo">加载中...</div></div>

<h2>配置管理</h2>
<div class="card"><div id="configPanel">加载中...</div></div>

<h2>审计日志</h2>
<div class="card"><div id="auditLog">加载中...</div></div>

<script>
async function fetchJSON(url,opts){
  try{
    const r=await fetch(url,opts);
    if(!r.ok) throw new Error(r.statusText);
    return await r.json();
  }catch(e){
    return null;
  }
}

function showError(msg){
  const el=document.getElementById('error');
  el.textContent=msg;el.style.display=msg?'block':'none';
}

async function loadSystem(){
  const d=await fetchJSON('/api/system');
  if(!d){document.getElementById('systemInfo').textContent='无法连接';return;}
  document.getElementById('systemInfo').innerHTML=
    '<div class="metric"><div class="label">地址</div><div class="value">'+(d.address||'local')+'</div></div>'+
    '<div class="metric"><div class="label">Actor 数量</div><div class="value">'+d.actor_count+'</div></div>';
}

async function loadCluster(){
  const d=await fetchJSON('/api/cluster');
  if(!d){document.getElementById('clusterInfo').textContent='未配置集群';return;}
  let html='<div class="metric"><div class="label">集群名</div><div class="value">'+d.name+'</div></div>';
  html+='<div class="metric"><div class="label">节点数</div><div class="value">'+d.members.length+'</div></div>';
  if(d.self)html+='<div class="metric"><div class="label">本节点</div><div class="value">'+d.self.address+'</div></div>';
  if(d.members&&d.members.length>0){
    html+='<table><tr><th>地址</th><th>ID</th><th>状态</th><th>Kinds</th><th>最后活跃</th></tr>';
    d.members.forEach(m=>{
      const cls='status-'+m.status.toLowerCase();
      html+='<tr><td>'+m.address+'</td><td>'+m.id+'</td><td class="'+cls+'">'+m.status+'</td><td>'+(m.kinds||[]).join(', ')+'</td><td>'+(m.last_seen||'-')+'</td></tr>';
    });
    html+='</table>';
  }
  document.getElementById('clusterInfo').innerHTML=html;
}

async function loadRuntime(){
  const d=await fetchJSON('/api/runtime');
  if(!d){document.getElementById('runtimeInfo').textContent='无法加载';return;}
  document.getElementById('runtimeInfo').innerHTML=
    '<div class="metric"><div class="label">Go 版本</div><div class="value">'+d.go_version+'</div></div>'+
    '<div class="metric"><div class="label">Goroutine</div><div class="value">'+d.num_goroutine+'</div></div>'+
    '<div class="metric"><div class="label">CPU 核心</div><div class="value">'+d.num_cpu+'</div></div>'+
    '<div class="metric"><div class="label">堆内存</div><div class="value">'+d.heap_alloc_mb.toFixed(2)+' MB</div></div>'+
    '<div class="metric"><div class="label">系统内存</div><div class="value">'+d.sys_mb.toFixed(2)+' MB</div></div>'+
    '<div class="metric"><div class="label">栈内存</div><div class="value">'+d.stack_inuse_mb.toFixed(2)+' MB</div></div>'+
    '<div class="metric"><div class="label">GC 次数</div><div class="value">'+d.num_gc+'</div></div>'+
    '<div class="metric"><div class="label">最近 GC 暂停</div><div class="value">'+d.gc_pause_ms.toFixed(3)+' ms</div></div>'+
    '<div class="metric"><div class="label">GC CPU</div><div class="value">'+d.gc_cpu_percent.toFixed(3)+' %</div></div>';
}

async function loadTopology(){
  const d=await fetchJSON('/api/actors/topology');
  if(!d){document.getElementById('actorTopology').textContent='无法加载';return;}
  if(d.length===0){document.getElementById('actorTopology').textContent='无 Actor';return;}
  function renderTree(nodes,depth){
    let html='';
    nodes.forEach(n=>{
      const indent='&nbsp;'.repeat(depth*4);
      const icon=n.children&&n.children.length>0?'&#9654; ':'&#9679; ';
      html+='<div style="font-family:monospace;padding:2px 0;font-size:14px">'+indent+icon+n.pid+'</div>';
      if(n.children)html+=renderTree(n.children,depth+1);
    });
    return html;
  }
  document.getElementById('actorTopology').innerHTML=renderTree(d,0);
}

async function loadActors(){
  const d=await fetchJSON('/api/actors');
  if(!d){document.getElementById('actorList').textContent='无法加载';return;}
  if(d.length===0){document.getElementById('actorList').textContent='无 Actor';return;}
  let html='<table><tr><th>PID</th><th>子节点</th></tr>';
  d.forEach(a=>{
    html+='<tr><td>'+a.pid+'</td><td>'+(a.children&&a.children.length>0?a.children.join(', '):'-')+'</td></tr>';
  });
  html+='</table>';
  document.getElementById('actorList').innerHTML=html;
}

async function loadHotActors(){
  const d=await fetchJSON('/api/hotactors?n=20');
  if(!d){document.getElementById('hotActors').textContent='未配置';return;}
  if(d.length===0){document.getElementById('hotActors').textContent='暂无数据';return;}
  let html='<table><tr><th>PID</th><th>消息数</th><th>平均延迟</th><th>总延迟</th><th>最后活跃</th></tr>';
  d.forEach(a=>{
    const avgMs=(a.avg_latency_ns/1e6).toFixed(2);
    const totalMs=(a.total_latency_ns/1e6).toFixed(2);
    html+='<tr><td>'+a.pid+'</td><td>'+a.msg_count+'</td><td>'+avgMs+' ms</td><td>'+totalMs+' ms</td><td>'+new Date(a.last_msg_time).toLocaleTimeString()+'</td></tr>';
  });
  html+='</table>';
  document.getElementById('hotActors').innerHTML=html;
}

async function loadMetrics(){
  const d=await fetchJSON('/api/metrics');
  if(!d){document.getElementById('metricsInfo').textContent='未配置';return;}
  let html='<table><tr><th>消息类型</th><th>计数</th><th>总延迟 (ms)</th><th>平均延迟 (ms)</th></tr>';
  const types=Object.keys(d.MsgCount||{}).sort();
  types.forEach(t=>{
    const count=d.MsgCount[t]||0;
    const lat=d.TotalLatency[t]||0;
    const avg=count>0?(lat/count/1e6).toFixed(2):'0';
    html+='<tr><td>'+t+'</td><td>'+count+'</td><td>'+(lat/1e6).toFixed(2)+'</td><td>'+avg+'</td></tr>';
  });
  html+='</table>';
  document.getElementById('metricsInfo').innerHTML=html;
}

// ---- 消息流量趋势图 ----
const chartColors=['#0ff','#f39c12','#2ecc71','#e74c3c','#9b59b6','#3498db','#e67e22','#1abc9c'];
async function loadTrafficChart(){
  const d=await fetchJSON('/api/metrics/history');
  const canvas=document.getElementById('trafficChart');
  const ctx=canvas.getContext('2d');
  const W=canvas.width,H=canvas.height;
  ctx.clearRect(0,0,W,H);
  ctx.fillStyle='#16213e';ctx.fillRect(0,0,W,H);

  if(!d||d.length===0){
    ctx.fillStyle='#7fdbff';ctx.font='14px sans-serif';
    ctx.fillText('暂无趋势数据',W/2-50,H/2);return;
  }

  // 收集所有消息类型
  const types=new Set();
  d.forEach(p=>{if(p.msg_rate)Object.keys(p.msg_rate).forEach(t=>types.add(t))});
  const typeArr=[...types].sort();

  if(typeArr.length===0){
    ctx.fillStyle='#7fdbff';ctx.font='14px sans-serif';
    ctx.fillText('暂无消息流量',W/2-50,H/2);return;
  }

  // 计算最大速率
  let maxRate=1;
  d.forEach(p=>{if(p.msg_rate)Object.values(p.msg_rate).forEach(v=>{if(v>maxRate)maxRate=v})});

  const pad={l:60,r:20,t:20,b:40};
  const cw=W-pad.l-pad.r,ch=H-pad.t-pad.b;

  // 坐标轴
  ctx.strokeStyle='#333';ctx.lineWidth=1;
  ctx.beginPath();ctx.moveTo(pad.l,pad.t);ctx.lineTo(pad.l,H-pad.b);ctx.lineTo(W-pad.r,H-pad.b);ctx.stroke();

  // Y轴刻度
  ctx.fillStyle='#7fdbff';ctx.font='11px sans-serif';ctx.textAlign='right';
  for(let i=0;i<=4;i++){
    const y=pad.t+ch*(1-i/4);
    const v=(maxRate*i/4).toFixed(1);
    ctx.fillText(v+'/s',pad.l-5,y+4);
    ctx.strokeStyle='#222';ctx.beginPath();ctx.moveTo(pad.l,y);ctx.lineTo(W-pad.r,y);ctx.stroke();
  }

  // X轴时间
  ctx.textAlign='center';
  const step=Math.max(1,Math.floor(d.length/6));
  for(let i=0;i<d.length;i+=step){
    const x=pad.l+cw*i/(d.length-1||1);
    const t=new Date(d[i].ts);
    ctx.fillText(t.toLocaleTimeString(),x,H-pad.b+16);
  }

  // 画线
  typeArr.forEach((type,ti)=>{
    ctx.strokeStyle=chartColors[ti%chartColors.length];
    ctx.lineWidth=2;ctx.beginPath();
    d.forEach((p,i)=>{
      const x=pad.l+cw*i/(d.length-1||1);
      const rate=(p.msg_rate&&p.msg_rate[type])||0;
      const y=pad.t+ch*(1-rate/maxRate);
      if(i===0)ctx.moveTo(x,y);else ctx.lineTo(x,y);
    });
    ctx.stroke();
  });

  // 图例
  ctx.font='11px sans-serif';ctx.textAlign='left';
  typeArr.forEach((type,ti)=>{
    const x=pad.l+10+ti*120;
    ctx.fillStyle=chartColors[ti%chartColors.length];
    ctx.fillRect(x,pad.t+2,12,3);
    ctx.fillText(type,x+16,pad.t+8);
  });
}

// ---- 集群拓扑图 ----
async function loadClusterGraph(){
  const d=await fetchJSON('/api/cluster/graph');
  const canvas=document.getElementById('clusterGraph');
  const ctx=canvas.getContext('2d');
  const W=canvas.width,H=canvas.height;
  ctx.clearRect(0,0,W,H);ctx.fillStyle='#16213e';ctx.fillRect(0,0,W,H);

  if(!d||!d.nodes||d.nodes.length===0){
    ctx.fillStyle='#7fdbff';ctx.font='14px sans-serif';
    ctx.fillText('未配置集群',W/2-40,H/2);return;
  }

  const statusColor={alive:'#2ecc71',suspect:'#f39c12',dead:'#e74c3c',left:'#95a5a6'};
  const nodes=d.nodes;const edges=d.edges||[];
  const cx=W/2,cy=H/2,radius=Math.min(W,H)/2-50;

  // 环形布局
  nodes.forEach((n,i)=>{
    const angle=2*Math.PI*i/nodes.length-Math.PI/2;
    n.x=cx+radius*Math.cos(angle);
    n.y=cy+radius*Math.sin(angle);
  });

  // 画连线
  ctx.strokeStyle='#0f3460';ctx.lineWidth=1;
  edges.forEach(e=>{
    const from=nodes.find(n=>n.id===e.from);
    const to=nodes.find(n=>n.id===e.to);
    if(from&&to){
      ctx.beginPath();ctx.moveTo(from.x,from.y);ctx.lineTo(to.x,to.y);ctx.stroke();
    }
  });

  // 画节点
  nodes.forEach(n=>{
    ctx.beginPath();ctx.arc(n.x,n.y,18,0,Math.PI*2);
    ctx.fillStyle=statusColor[n.status.toLowerCase()]||'#95a5a6';ctx.fill();
    ctx.strokeStyle='#0ff';ctx.lineWidth=1;ctx.stroke();
    // 标签
    ctx.fillStyle='#e0e0e0';ctx.font='11px sans-serif';ctx.textAlign='center';
    ctx.fillText(n.address,n.x,n.y+32);
    ctx.fillStyle='#fff';ctx.font='bold 10px sans-serif';
    ctx.fillText(n.status,n.x,n.y+4);
  });
}

// ---- Actor 火焰图 ----
async function loadFlameGraph(){
  const d=await fetchJSON('/api/actors/flamegraph');
  const canvas=document.getElementById('flameGraph');
  const ctx=canvas.getContext('2d');
  const W=canvas.width,H=canvas.height;
  ctx.clearRect(0,0,W,H);ctx.fillStyle='#16213e';ctx.fillRect(0,0,W,H);

  if(!d||d.value===0){
    ctx.fillStyle='#7fdbff';ctx.font='14px sans-serif';
    ctx.fillText('暂无火焰图数据',W/2-50,H/2);return;
  }

  const barH=22,pad=4;
  const maxDepth=getDepth(d);
  const scaleH=Math.min(barH,Math.floor((H-20)/maxDepth));

  function getDepth(node){
    if(!node.children||node.children.length===0)return 1;
    return 1+Math.max(...node.children.map(getDepth));
  }

  const flameColors=['#e25822','#e87d2e','#f0a030','#d44','#c33','#e63','#f80','#fc0'];

  function drawNode(node,x,w,depth){
    const y=H-scaleH*(depth+1)-2;
    if(w<1)return;

    const ci=(depth+node.name.length)%flameColors.length;
    ctx.fillStyle=flameColors[ci];
    ctx.fillRect(x,y,w-1,scaleH-2);

    // 文字
    if(w>30){
      ctx.fillStyle='#fff';ctx.font='11px sans-serif';ctx.textAlign='left';
      const label=node.name+' ('+node.value+')';
      const maxChars=Math.floor((w-4)/6.5);
      ctx.fillText(label.substring(0,maxChars),x+3,y+scaleH/2+3);
    }

    if(node.children){
      let cx=x;
      node.children.forEach(child=>{
        const cw=w*child.value/node.value;
        drawNode(child,cx,cw,depth+1);
        cx+=cw;
      });
    }
  }

  drawNode(d,pad,W-pad*2,0);
}

// ---- 配置管理 ----
async function loadConfig(){
  const d=await fetchJSON('/api/config');
  if(!d){document.getElementById('configPanel').textContent='未配置';return;}
  if(d.length===0){document.getElementById('configPanel').textContent='无已注册配置';return;}
  let html='<table><tr><th>文件名</th><th>类型</th><th>操作</th></tr>';
  d.forEach(c=>{
    html+='<tr><td>'+c.filename+'</td><td>'+c.type+'</td><td><button class="reload-btn" onclick="reloadConfig(\''+c.filename+'\')">重载</button></td></tr>';
  });
  html+='</table>';
  document.getElementById('configPanel').innerHTML=html;
}

async function reloadConfig(filename){
  const r=await fetchJSON('/api/config/reload',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({filename})});
  if(r&&r.status==='ok'){
    loadConfig();loadAudit();
  }else{
    alert('重载失败: '+(r&&r.error||'unknown'));
  }
}

// ---- 审计日志 ----
async function loadAudit(){
  const d=await fetchJSON('/api/audit?n=20');
  if(!d){document.getElementById('auditLog').textContent='未配置';return;}
  if(d.length===0){document.getElementById('auditLog').textContent='暂无审计记录';return;}
  let html='<table><tr><th>时间</th><th>操作</th><th>详情</th><th>来源</th></tr>';
  d.forEach(e=>{
    html+='<tr><td>'+new Date(e.time).toLocaleString()+'</td><td>'+e.action+'</td><td>'+e.detail+'</td><td>'+e.source+'</td></tr>';
  });
  html+='</table>';
  document.getElementById('auditLog').innerHTML=html;
}

// ---- 日志级别 ----
async function loadLogLevel(){
  const d=await fetchJSON('/api/log/level');
  if(d&&d.level){
    document.getElementById('logLevel').value=d.level.toLowerCase();
  }
}

async function changeLogLevel(level){
  await fetchJSON('/api/log/level',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({level})});
  loadAudit();
}

function refreshAll(){
  showError('');
  loadSystem();loadCluster();loadRuntime();loadTopology();loadActors();loadHotActors();loadMetrics();
  loadTrafficChart();loadClusterGraph();loadFlameGraph();loadConfig();loadAudit();loadLogLevel();
}

refreshAll();
setInterval(()=>{
  if(document.getElementById('autoRefresh').checked)refreshAll();
},5000);
</script>
</body>
</html>`
