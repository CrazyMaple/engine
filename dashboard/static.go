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
.toolbar{display:flex;justify-content:space-between;align-items:center;margin-bottom:20px}
.auto-refresh{color:#7fdbff;font-size:13px}
#error{color:#e74c3c;padding:10px;display:none}
</style>
</head>
<body>
<div class="toolbar">
<h1>MapleWish Engine Dashboard</h1>
<div>
<label class="auto-refresh"><input type="checkbox" id="autoRefresh" checked> 自动刷新 (5s)</label>
<button class="refresh-btn" onclick="refreshAll()">刷新</button>
</div>
</div>
<div id="error"></div>

<div class="grid">
<div class="card" id="systemCard"><h3>系统概览</h3><div id="systemInfo">加载中...</div></div>
<div class="card" id="clusterCard"><h3>集群状态</h3><div id="clusterInfo">加载中...</div></div>
</div>

<h2>Actor 列表</h2>
<div class="card"><div id="actorList">加载中...</div></div>

<h2>热点 Actor (Top 20)</h2>
<div class="card"><div id="hotActors">加载中...</div></div>

<h2>消息指标</h2>
<div class="card"><div id="metricsInfo">加载中...</div></div>

<script>
async function fetchJSON(url){
  try{
    const r=await fetch(url);
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

function refreshAll(){
  showError('');
  loadSystem();loadCluster();loadActors();loadHotActors();loadMetrics();
}

refreshAll();
setInterval(()=>{
  if(document.getElementById('autoRefresh').checked)refreshAll();
},5000);
</script>
</body>
</html>`
