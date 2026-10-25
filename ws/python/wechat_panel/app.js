'use strict';
const $ = id => document.getElementById(id);
const groups = {cdn:'媒体上传 / 下载',favor:'收藏',finder:'视频号',friend:'联系人 / 好友',group:'群管理',label:'标签',msg:'消息收发',official:'公众号',openim:'企业微信联系人',search:'搜索',sns:'朋友圈',user:'个人信息',wxapp:'小程序业务'};
let catalog, current, supported = new Set(), connected = false, busy = false, fields = [], resultText = '', generation = 0, stateGeneration = 0;
let selectedGroup = '', selectedMode = 'call';
let inboxSocket, inboxCount = 0;
const pretty = v => JSON.stringify(v, null, 2);
function node(tag, text, className) { const n=document.createElement(tag); if(text !== undefined)n.textContent=text; if(className)n.className=className; return n; }
const mainTabs = {messages:['tab-messages','panel-messages'],explorer:['tab-explorer','panel-explorer'],results:['tab-results','panel-results']};
function activateTab(name, focus=false){
  for(const [key,[tabId,panelId]] of Object.entries(mainTabs)){
    const active=key===name;
    $(tabId).setAttribute('aria-selected',String(active));
    $(tabId).tabIndex=active?0:-1;
    $(panelId).hidden=!active;
  }
  closeGroupMenu();
  if(focus)$(mainTabs[name][0]).focus();
  window.scrollTo(0,0);
}
for(const [name,[tabId]] of Object.entries(mainTabs)){
  $(tabId).onclick=()=>activateTab(name);
  $(tabId).onkeydown=e=>{
    const names=Object.keys(mainTabs), index=names.indexOf(name);
    const next=e.key==='ArrowRight'?names[(index+1)%names.length]:e.key==='ArrowLeft'?names[(index+names.length-1)%names.length]:e.key==='Home'?names[0]:e.key==='End'?names[names.length-1]:null;
    if(next){e.preventDefault();activateTab(next,true);}
  };
}
async function api(path, body) {
  const r=await fetch(path,body===undefined ? {} : {method:'POST',headers:{'Content-Type':'application/json','X-Panel-Request':'1'},body:JSON.stringify(body)});
  let data; try { data=await r.json(); } catch { throw new Error(`HTTP ${r.status}`); }
  if(!r.ok) throw new Error(`${data.code || r.status}: ${data.message || '请求失败'}`);
  return data;
}
let requestKey=crypto.randomUUID(), lastRequest=null, lastCallId=null;
function payload(){return {method:current.name,account_id:$('account').value,arguments:$('arguments').value,mode:selectedMode,idempotency_key:requestKey};}
function updateExecute(){
  const account=$('account').value;
  const online=$('account').selectedOptions[0]?.dataset.online==='true';
  const enabled=connected && account && online && supported.has(current?.name);
  $('execute').disabled=busy || !enabled;
  $('account').disabled=busy;
  $('execute').textContent=busy?'调用中…':'执行调用 →';
  $('availability').textContent=!connected?'未连接':!account?'待选择账号':!online?'账号离线':supported.has(current?.name)?'可调用':'未开放';
  $('availability').className='badge'+(enabled?'':' neutral');
  $('target-hint').textContent=!account?'请先在右上角选择账号':!online?'所选账号离线':`目标：${account}`;
  $('target-hint').title=account;
}
function renderNav(){
  const q=$('search').value.trim().toLowerCase(), group=selectedGroup;
  const items=catalog.methods.filter(m=>(!group || m.group===group) && `${m.name} ${m.title} ${m.description} ${m.fields.map(f=>f.name).join(' ')}`.toLowerCase().includes(q));
  $('count').textContent=`${items.length} / ${catalog.methods.length}`;
  $('methods').replaceChildren(...(items.length ? items.map(m=>{const b=node('button'); b.append(node('strong',m.title),node('small',m.name)); b.classList.toggle('active',m.name===current?.name); b.onclick=()=>select(m); return b;}) : [node('p','没有匹配的接口，请尝试其他关键词或分类。','nav-empty')]));
}
function messageRow(frame){
  const message=frame.message || {}, raw=message.raw || {};
  const peer=message.peer_id || message.peer || '';
  const row=node('article',undefined,'message-entry');
  const body=node('div',undefined,'message-body');
  const meta=node('div',undefined,'message-meta');
  const sender=message.from?.id || peer || '未知发送者';
  meta.append(node('strong',sender));
  meta.append(node('span',`账号 · ${frame.account?.id || frame.account_id || ''}`,'message-account'));
  if(peer.endsWith('@chatroom'))meta.append(node('span',`群聊 · ${peer}`));
  else if(peer && peer!==sender)meta.append(node('span',`会话 · ${peer}`));
  const date=new Date(Number(message.timestamp || frame.received_at)*1000);
  const when=node('time',Number.isNaN(date.getTime())?'时间未知':date.toLocaleString('zh-CN'),'message-time');
  meta.append(when);
  const content=typeof raw.content==='string'?raw.content:message.text || JSON.stringify(raw && Object.keys(raw).length?raw:message);
  const details=node('details',undefined,'message-raw');
  details.append(node('summary','查看原始事件'),node('pre',JSON.stringify(frame,null,2)));
  body.append(meta,node('p',content,'message-text'),details);
  row.append(node('span',peer.endsWith('@chatroom')?'群':'收','message-avatar'),body);
  row.title=`事件 ID：${frame.event_id || '未知'}`;
  return row;
}
function connectMessages(){
  const protocol=location.protocol==='https:'?'wss:':'ws:';
  inboxSocket=new WebSocket(`${protocol}//${location.host}/api/events`);
  $('message-status').textContent='正在连接消息推送…';
  inboxSocket.onopen=()=>{$('message-status').textContent='正在接收所有账号的新消息';};
  inboxSocket.onmessage=event=>{
    const frame=JSON.parse(event.data);
    if(frame.event!=='wechat.message.received')return;
    const list=$('message-list');
    list.querySelector('.message-empty')?.remove();
    list.prepend(messageRow(frame));
    inboxCount+=1;
    $('message-count').textContent=String(inboxCount);
    $('tab-message-count').textContent=String(inboxCount);
  };
  inboxSocket.onclose=()=>{
    $('message-status').textContent='消息连接中断，正在重连…';
    setTimeout(connectMessages,2000);
  };
}
function closeGroupMenu(restoreFocus=false){
  $('group-menu').hidden=true;
  $('group-toggle').setAttribute('aria-expanded','false');
  if(restoreFocus)$('group-toggle').focus();
}
function openGroupMenu(){
  $('group-menu').hidden=false;
  $('group-toggle').setAttribute('aria-expanded','true');
  $('group-menu').querySelector('[aria-pressed="true"]')?.focus();
}
function renderGroups(){
  const counts=new Map();
  for(const method of catalog.methods)counts.set(method.group,(counts.get(method.group)||0)+1);
  const choices=[['','全部分类',catalog.methods.length],...[...counts].map(([key,count])=>[key,groups[key]||key,count])];
  $('group-menu').replaceChildren(...choices.map(([key,label,count])=>{
    const button=node('button',undefined,'group-option');
    button.type='button';button.dataset.group=key;button.setAttribute('aria-pressed',String(key===selectedGroup));
    button.append(node('span',label),node('span',String(count),'group-count'));
    button.onclick=()=>{
      selectedGroup=key;$('group-label').textContent=label;
      for(const option of $('group-menu').children)option.setAttribute('aria-pressed',String(option.dataset.group===key));
      closeGroupMenu(true);renderNav();$('methods').scrollTop=0;
    };
    return button;
  }));
}
$('group-toggle').onclick=()=>$('group-menu').hidden?openGroupMenu():closeGroupMenu();
$('start-search').onclick=()=>{$('search').focus();$('search').scrollIntoView({block:'nearest'});};
$('call-mode').onclick=e=>{
  const button=e.target.closest('button[data-mode]');
  if(!button)return;
  selectedMode=button.dataset.mode;
  for(const option of $('call-mode').querySelectorAll('button'))option.setAttribute('aria-pressed',String(option===button));
};
$('group-menu').onkeydown=e=>{
  const options=[...$('group-menu').querySelectorAll('button')];
  const index=options.indexOf(document.activeElement);
  if(e.key==='Escape'){e.preventDefault();closeGroupMenu(true);}
  if(e.key==='ArrowDown' || e.key==='ArrowUp'){
    e.preventDefault();options[(index+(e.key==='ArrowDown'?1:-1)+options.length)%options.length].focus();
  }
};
document.addEventListener('pointerdown',e=>{if(!e.target.closest('.group-filter'))closeGroupMenu();});
function scalarType(schema){return Array.isArray(schema.type)?schema.type.find(t=>t!=='null'):schema.type || 'any';}
function argumentsFromFields(){
  // Build numeric JSON literals as text: never round i64 values through JS Number.
  const values=[];
  for(const {field,input,include,type} of fields){
    if(!include.checked) continue;
    let raw;
    if(type==='string') raw=JSON.stringify(input.value);
    else if(type==='boolean') raw=input.value;
    else raw=input.value.trim() || 'null';
    values.push(`${JSON.stringify(field.name)}: ${raw}`);
  }
  return values.length?`{\n  ${values.join(',\n  ')}\n}`:'{}';
}
function syncFields(){
  generation++;
  $('arguments').value=argumentsFromFields();
  $('validation').textContent='表单已更新 JSON；执行前由 Python 校验类型和必填字段。';
  $('python').textContent='点击“校验参数 / 生成 Python 代码”更新示例。';
}
function renderFields(){
  fields=[]; $('fields').replaceChildren();
  const example=current.example;
  current.fields.forEach((field,i)=>{
    const wrap=node('div',undefined,'field'), heading=node('div',undefined,'field-heading');
    const label=node('label',field.name); label.htmlFor=`field-${i}`;
    const type=scalarType(field.schema), include=node('input'); include.type='checkbox';
    include.checked=field.required===true;
    include.disabled=field.required===true;
    const opt=node('label',undefined,'optional'); opt.append(include,document.createTextNode('传递此参数'));
    heading.append(label,node('span',type,'type'),node('span',field.required===true?'必填':field.required===null?'按业务需要':'可省略','required'));
    if(field.required!==true)heading.append(opt);
    wrap.append(heading,node('p',field.description));
    let input;
    if(type==='boolean'){input=node('select'); for(const v of ['false','true','null']){if(v==='null'&&!Array.isArray(field.schema.type))continue; const o=node('option',v);o.value=v;input.append(o);}}
    else {const multiline=type==='object' || type==='array' || /base64|b64|xml|content/i.test(field.name);input=node(multiline?'textarea':'input'); if(input.tagName==='INPUT')input.type='text'; input.spellcheck=false;}
    input.id=`field-${i}`;
    const value=example[field.name];
    input.value=type==='string'?(value ?? ''):value===undefined?(type==='boolean'?'false':''):JSON.stringify(value);
    input.disabled=!include.checked;
    const onChange=()=>{
      input.disabled=!include.checked; syncFields();
    };
    include.onchange=onChange; input.oninput=syncFields;
    wrap.append(input);
    if(/base64|b64/i.test(field.name)){
      const file=node('input'); file.type='file';file.setAttribute('aria-label',`${field.name} 选择文件转 Base64`);
      file.onchange=async()=>{
        const selected=file.files[0]; if(!selected)return;
        if(selected.size>22*1024*1024){$('validation').textContent='单个文件不能超过 22 MiB；所有参数编码后还受宿主 WS 帧限制。';return;}
        try{const raw=await new Promise((resolve,reject)=>{const r=new FileReader();r.onload=()=>resolve(r.result);r.onerror=reject;r.readAsDataURL(selected);}); input.value=raw.slice(raw.indexOf(',')+1);include.checked=true;onChange();}
        catch{$('validation').textContent='文件读取失败';}
      };
      wrap.append(file);
    }
    if(field.schema.properties || field.schema.items){const detail=node('details');detail.append(node('summary','展开嵌套参数结构'),node('pre',pretty(field.schema)));wrap.append(detail);}
    fields.push({field,input,include,type}); $('fields').append(wrap);
  });
  if(!fields.length)$('fields').append(node('p','此方法不需要业务参数。','muted'));
}
function select(method){
  generation++; current=method;
  $('method-empty').hidden=true;$('method-workspace').hidden=false;
  $('category').textContent=`${groups[method.group] || method.group} / 业务 API`;
  $('title').textContent=method.title; $('method-name').textContent=method.name;
  $('description').textContent=method.description; $('body-description').textContent=method.body_description || '';
  $('route').textContent=method.path || '宿主本地能力查询';
  $('parameter-note').textContent='可省略和按业务需要的参数默认不传；勾选后才加入 JSON。具体约束以服务端返回为准。';
  $('validation').textContent='';
  $('field-count').textContent=String(method.fields.length);$('code-details').open=false;
  $('return-note').textContent=method.return_note;
  $('response-description').textContent=method.response_description;
  $('response-schema').textContent=Object.keys(method.response_schema).length?pretty(method.response_schema):'服务文档未给出具体 data schema；查看实际结果。';
  $('python').textContent='选择账号后，点击“校验参数 / 生成 Python 代码”。';
  renderFields();$('arguments').value=argumentsFromFields();renderNav();updateExecute();activateTab('explorer');
  document.querySelector('.explorer-main').scrollTop=0;
}
async function refresh(){
  const ticket=++stateGeneration, selected=$('account').value;
  supported.clear();updateExecute();$('refresh').disabled=true;
  try{
    const state=await api(`/api/state?account_id=${encodeURIComponent(selected)}`);
    if(ticket!==stateGeneration)return;
    const placeholder=node('option',state.accounts.length?'请选择微信账号':'宿主没有微信账号');placeholder.value='';
    $('account').replaceChildren(placeholder,...state.accounts.map(a=>{const o=node('option',`${a.display_name || a.account_id} · ${a.account_id} · ${a.online?'在线':'离线'}`);o.value=a.account_id;o.dataset.online=String(a.online);return o;}));
    // Never switch to another account implicitly, including after removal/offline/reconnect.
    $('account').value=state.accounts.some(a=>a.account_id===selected)?selected:'';
    connected=true;supported=new Set(state.supported);
    $('status').textContent=`已连接 · ${state.accounts.length} 个账号`;
    $('notice').textContent=state.accounts.length?'':'宿主没有微信账号，请在宿主登录后刷新。';
    if(selected && !$('account').value)$('notice').textContent='原选择的账号已不可用，请重新选择操作账号。';
  }catch(e){if(ticket!==stateGeneration)return;connected=false;supported.clear();$('status').textContent='连接断开';$('notice').textContent=e.message;}
  finally{if(ticket===stateGeneration){$('refresh').disabled=false;updateExecute();}}
}
function showResultView(view){
  let truncated=false;
  function render(item,depth=0){
    if(item.truncated)truncated=true;
    if(item.type==='object' || item.type==='array'){
      const wrap=node('details',undefined,'tree-node');wrap.open=depth<2;
      const summary=node('summary');
      if(item.key!==undefined)summary.append(node('span',item.key+': ','tree-key'));
      summary.append(node('span',item.type==='array'?'[ ]':'{ }','tree-count'),node('span',`  ${item.count} 项`,'tree-count'));
      const children=node('div',undefined,'tree-children');
      for(const child of item.children)children.append(render(child,depth+1));
      if(item.truncated)children.append(node('div','部分内容已折叠省略，请查看原始 JSON。','tree-more'));
      wrap.append(summary,children);return wrap;
    }
    const row=node('div',undefined,'tree-scalar');
    if(item.key!==undefined)row.append(node('span',item.key+':','tree-key'));
    row.append(node('span',(item.type==='string'?JSON.stringify(item.value):item.value)+(item.truncated?' …':''),`tree-${item.type}`));
    return row;
  }
  $('result-tree').replaceChildren(render(view));
  $('result-note').textContent=truncated?'结构视图已截短长文本或大集合；原始 JSON、复制和下载保留完整数据。':'完整原始数据可复制或下载；请求返回不代表业务已成功，请查看结果字段。';
}
function resultTab(raw){
  $('result').hidden=!raw;$('result-tree').hidden=raw;
  $('tree-tab').setAttribute('aria-selected',String(!raw));$('raw-tab').setAttribute('aria-selected',String(raw));
}
$('tree-tab').onclick=()=>resultTab(false);$('raw-tab').onclick=()=>resultTab(true);
$('search').oninput=renderNav;$('refresh').onclick=refresh;$('account').onchange=()=>{generation++;$('python').textContent='账号已切换，请重新生成调用代码。';refresh();};
$('example').onclick=()=>select(current);
$('arguments').oninput=()=>{generation++;$('validation').textContent='JSON 已修改；执行以此编辑器内容为准。';$('python').textContent='点击校验按钮更新代码。';};
$('preview').onclick=async()=>{const ticket=generation;try{const p=await api('/api/preview',payload());if(ticket!==generation)return;$('python').textContent=p.python;$('arguments').value=p.arguments;$('validation').textContent='参数校验通过。';$('code-details').open=true;}catch(e){$('validation').textContent=e.message;}};
$('execute').onclick=async()=>{
  if(busy || $('execute').disabled)return;
  requestKey=crypto.randomUUID();const request=payload();lastRequest=request;lastCallId=null;$('query-receipt').disabled=false;busy=true;updateExecute();
  activateTab('results');
  $('result-meta').textContent=`${request.method}\n${request.account_id}`;
  $('result-status').textContent='调用中';$('tab-result-status').textContent='调用中';$('result-status').className='badge pending';
  $('copy-result').disabled=true;$('download').disabled=true;
  $('result-tree').replaceChildren(node('div','等待宿主返回…','empty-result'));
  $('result').textContent='等待宿主返回…';$('result-size').textContent='';
  try{
    const r=await api('/api/call',request);resultText=r.result_json;$('result').textContent=resultText;
    $('result-meta').textContent=`${r.method} · ${r.elapsed_ms} ms\n账号：${r.account_id}`;
    lastCallId=r.call_id;$('result-status').textContent=r.status;$('tab-result-status').textContent=r.status;$('result-status').className='badge'+(['failed','rejected','unknown','partial'].includes(r.status)?' error':'');
    $('result-meta').textContent+=`\nCall: ${r.call_id}\n幂等键: ${request.idempotency_key}`;
    // Older running servers may not provide the structured view yet.
    if(r.view)showResultView(r.view);else{resultTab(true);$('result-tree').replaceChildren(node('p','请重启面板服务以启用结构视图。','muted'));}
  }catch(e){
    resultText=pretty({error:e.message});$('result').textContent=resultText;
    showResultView({type:'object',count:1,children:[{key:'error',type:'string',value:e.message}]});
    $('result-status').textContent='调用失败';$('tab-result-status').textContent='调用失败';$('result-status').className='badge error';
    $('result-note').textContent='连接中断不代表调用失败；请点击查询回执，按本次幂等键查找，不要重复发送。';
  }finally{
    busy=false;$('download').disabled=false;$('copy-result').disabled=false;
    const bytes=new TextEncoder().encode(resultText).length;
    $('result-size').textContent=bytes<1024?`${bytes} B`:`${(bytes/1024).toFixed(1)} KB`;
    updateExecute();
  }
};
$('copy-result').onclick=async()=>{try{await navigator.clipboard.writeText(resultText);$('result-note').textContent='完整 JSON 已复制。';}catch{$('result-note').textContent='复制失败，请使用下载或原始 JSON 视图。';}};
$('download').onclick=()=>{const url=URL.createObjectURL(new Blob([resultText],{type:'application/json'}));const a=node('a');a.href=url;a.download='wechat-result.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);};
$('copy-code').onclick=async()=>{try{await navigator.clipboard.writeText($('python').textContent);$('validation').textContent='Python 代码已复制。';}catch{$('validation').textContent='请选中代码手动复制。';}};
(async()=>{try{catalog=await api('/api/catalog');$('mode').textContent=catalog.demo?'MOCK / 模拟环境':'WS / 真实宿主';renderGroups();renderNav();await refresh();}catch(e){$('notice').textContent=e.message;}})();

$('query-receipt').onclick=async()=>{
  if(!lastRequest)return;
  try{
    const r=await api('/api/receipt',{account_id:lastRequest.account_id,call_id:lastCallId,idempotency_key:lastRequest.idempotency_key});
    lastCallId=r.call_id;resultText=r.result_json;$('result').textContent=resultText;showResultView(r.view);
    $('result-status').textContent=r.status;$('tab-result-status').textContent=r.status;
    $('result-status').className='badge'+(['failed','rejected','unknown','partial'].includes(r.status)?' error':'');
    const bytes=new TextEncoder().encode(resultText).length;$('result-size').textContent=bytes<1024?`${bytes} B`:`${(bytes/1024).toFixed(1)} KB`;
  }catch(e){$('result-note').textContent=e.message;}
};
connectMessages();
