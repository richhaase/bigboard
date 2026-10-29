import {ranges,number,compact,identity,change,dateValue,selectCommits,uniqueCommits,totals,people,scopedCommits,activityRows,sortRows,dailySeries,filterPRs,safeURL} from './model.js';

const $=selector=>document.querySelector(selector);
const esc=value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const icons={activity:'▥',people:'◉',prs:'⑂',folder:'▰',file:'≡',repo:'▱',refresh:'↻',settings:'⚙',chevron:'›',back:'←',close:'×',link:'↗',search:'⌕'};
const icon=name=>`<span aria-hidden="true">${icons[name]||name}</span>`;
const api=async(method,arg)=>{
  if(window.go?.main?.App?.[method]) return window.go.main.App[method](...(arg===undefined?[]:[arg]));
  const paths={Snapshot:'snapshot',Refresh:'refresh',SaveSettings:'settings',Discover:'discover'};
  const response=await fetch(`/api/${paths[method]}`,{method:method==='Snapshot'?'GET':'POST',headers:{'Content-Type':'application/json'},...(arg===undefined?{}:{body:JSON.stringify(arg)})});
  if(!response.ok){let message=await response.text();try{message=JSON.parse(message).error||message;}catch{}throw new Error(message||`Request failed (${response.status})`);}
  return response.json();
};
const state={snapshot:null,board:'activity',days:30,repos:[],busy:false,error:'',activity:{query:'',sort:'change',reverse:false,hideBots:false,scope:{repo:'',path:'',file:false},selected:''},people:{query:'',sort:'change',reverse:false,hideBots:false,selected:''},prs:{query:'',sort:'updated',author:'',review:'',checks:'',state:'',mine:'',selected:''},modal:null,settingsDraft:null,discovered:[],dialogError:''};
let lastFocus=null;
let toastTimer;
const age=value=>{const ms=Date.now()-dateValue(value);if(!dateValue(value))return 'Unknown';const days=Math.floor(ms/86400000);return days<0?'Future date':days===0?'Today':days===1?'Yesterday':days<30?`${days}d ago`:new Date(value).toLocaleDateString(undefined,{month:'short',day:'numeric'});};
const fullDate=value=>dateValue(value)?new Date(value).toLocaleString(undefined,{dateStyle:'medium',timeStyle:'short'}):'Unavailable';
const initials=name=>(name||'?').split(/[\s@._-]+/).filter(Boolean).slice(0,2).map(p=>p[0]).join('').toUpperCase();
const statusName=value=>({APPROVED:'Approved',CHANGES_REQUESTED:'Changes requested',REVIEW_REQUIRED:'Needs review',SUCCESS:'Passing',PENDING:'Pending',FAILURE:'Failing',ERROR:'Error',UNKNOWN:'Unknown',EXPECTED:'Expected'}[value]||value?.replaceAll('_',' ').toLowerCase()||'Unknown');
const badge=(value,type='')=>`<span class="badge ${type||(['SUCCESS','APPROVED'].includes(value)?'good':['FAILURE','ERROR','CHANGES_REQUESTED'].includes(value)?'bad':['PENDING','REVIEW_REQUIRED','EXPECTED'].includes(value)?'pending':'muted')}">${esc(statusName(value))}</span>`;
const empty=(title,body)=>`<div class="empty"><span class="empty-symbol">▱</span><h3>${esc(title)}</h3><p>${esc(body)}</p></div>`;
const bar=(added,removed,max=added+removed)=>`<div class="change-bar" role="img" aria-label="${number(added)} lines added, ${number(removed)} removed"><span class="added" style="width:${Math.max(0,Math.min(100,added/Math.max(1,max)*100))}%"></span><span class="removed" style="width:${Math.max(0,Math.min(100,removed/Math.max(1,max)*100))}%"></span></div>`;
const delta=(added,removed,unknown)=>`<span class="delta"><span class="positive">+${compact(added)}</span><span class="negative">−${compact(removed)}</span>${unknown?'<span title="Some line counts are unavailable">?</span>':''}</span>`;
function toast(message){clearTimeout(toastTimer);$('#toast').textContent=message;$('#toast').classList.add('visible');toastTimer=setTimeout(()=>$('#toast').classList.remove('visible'),4500);}
function validSnapshot(data){return {...data,repositories:data.repositories||[],commits:data.commits||[],pullRequests:data.pullRequests||[],settings:{mode:'demo',personalAccount:'',organizations:[],repositories:[],...data.settings},status:{phase:'ready',message:'',errors:[],...data.status}};}
function currentCommits(){return selectCommits(state.snapshot,{days:state.days,repos:state.repos,hideBots:state[state.board]?.hideBots});}
function visibleRepositories(){return state.snapshot.repositories.filter(r=>!state.repos.length||state.repos.includes(r.id));}
function scopeTitle(){const scope=state.activity.scope;return scope.file?scope.path.split('/').pop():scope.path?scope.path.split('/').pop():scope.repo?scope.repo.split('/').pop():'Repository activity';}
function tableHead(columns){return `<div class="table-head">${columns.map(c=>`<span>${c}</span>`).join('')}</div>`;}
function render(){
 const snapshot=state.snapshot;if(!snapshot)return;
 const configs={activity:['Activity','Follow the change. Find the context.'],people:['Contributors','The people behind the work.'],prs:['Pull requests','See what is moving and what needs attention.']};
 const [title,subtitle]=configs[state.board];const total=totals(currentCommits());
 $('#app').innerHTML=`<aside class="sidebar"><div class="brand"><div class="brand-mark">▥</div><div><strong>BIGBOARD</strong><span>WORK IN VIEW</span></div></div><div class="workspace"><span class="workspace-dot"></span><div><strong>${esc(snapshot.mode==='demo'?'Demo workspace':snapshot.settings.personalAccount||snapshot.settings.organizations?.[0]||'Your workspace')}</strong><span>${snapshot.repositories.length} repositories</span></div></div><div class="nav-label">EXPLORE</div><nav aria-label="Boards">${[['activity','Activity','a'],['people','Contributors','c'],['prs','Pull requests','p']].map(([id,label,key])=>`<button data-action="board" data-value="${id}" aria-label="${label}" class="nav-item ${state.board===id?'active':''}" ${state.board===id?'aria-current="page"':''}>${icon(id)}<span>${label}</span><kbd>${key}</kbd></button>`).join('')}</nav><div class="sidebar-bottom">${snapshot.mode==='demo'?'<div class="demo-note"><span class="demo-label">DEMO</span><p>A synthetic workspace.<br>Explore freely.</p><button data-action="settings" class="text-button">Connect your repositories ↗</button></div>':''}<button class="nav-item" data-action="status" aria-label="Data status">${icon('◷')}<span>Data status</span><span class="status-dot ${snapshot.status.errors?.length?'warning':''}"></span></button><button class="nav-item" data-action="settings" aria-label="Settings">${icon('settings')}<span>Settings</span><kbd>,</kbd></button><button class="nav-item" data-action="help" aria-label="Keyboard guide">${icon('?')}<span>Keyboard guide</span><kbd>?</kbd></button><div class="prototype">DESKTOP PROTOTYPE <span>LOCAL FIRST</span></div></div></aside><main class="main"><header class="page-header"><div><div class="eyebrow">YOUR ENGINEERING LANDSCAPE</div><h1>${title}</h1><p>${subtitle}</p></div><button class="button refresh ${state.busy?'spinning':''}" data-action="refresh" ${state.busy?'disabled':''}>${icon('refresh')} ${state.busy?'Refreshing…':'Refresh'}<kbd>r</kbd></button></header><section class="scope-toolbar" aria-label="Shared viewing scope"><div class="date-tabs" role="group" aria-label="Date range">${ranges.map(r=>`<button data-action="range" data-value="${r.value}" class="${state.days===r.value?'selected':''}" aria-pressed="${state.days===r.value}">${r.label}</button>`).join('')}</div><button class="button scope-button" data-action="repositories">${icon('repo')} ${state.repos.length?`${state.repos.length} selected`:'All repositories'} <span>⌄</span></button><div class="snapshot-time"><span class="status-dot ${snapshot.status.errors?.length?'warning':''}"></span>${snapshot.mode==='demo'?'Demo data':snapshot.mode==='cache'?'Cached history':`Updated ${age(snapshot.generatedAt).toLowerCase()}`}</div></section>${state.error?`<div class="error-banner" role="alert">${esc(state.error)}<button data-action="dismiss-error" aria-label="Dismiss error">×</button></div>`:''}${snapshot.status.errors?.length?`<button class="coverage-banner" data-action="status">${snapshot.status.errors.length} data ${snapshot.status.errors.length===1?'issue':'issues'} · Some results may be incomplete <span>View details →</span></button>`:''}${state.board==='activity'?renderActivity(total):state.board==='people'?renderPeople(total):renderPRs()}<footer class="main-footer"><span>Activity is a signal, not a productivity score.</span><span>${state.board==='prs'?number(snapshot.pullRequests.length)+' open PRs in snapshot':number(total.commits)+' commits in scope'}<span class="footer-separator">/</span>${snapshot.mode==='demo'?'SYNTHETIC DATA':'LOCAL WORKSPACE'}</span></footer></main>`;
}
function filterToolbar(type,extra=''){
 const s=state[type];return `<div class="list-toolbar"><label class="search-field">${icon('search')}<input aria-label="Search ${type==='people'?'contributors':type==='prs'?'pull requests':'activity'}" data-field="query" data-board="${type}" placeholder="${type==='people'?'Find a contributor…':type==='prs'?'Search titles, authors, repositories…':'Find in this view…'}" value="${esc(s.query)}"><kbd>/</kbd></label>${extra}${type!=='prs'?`<label class="checkbox-label"><input type="checkbox" data-field="hideBots" data-board="${type}" ${s.hideBots?'checked':''}> Hide bots</label>`:''}<label class="sort-label"><span>Sort</span><select aria-label="Sort ${type}" data-field="sort" data-board="${type}">${(type==='prs'?[['updated','Recently updated'],['attention','Needs attention'],['repository','Repository']]:[['change','Lines changed'],['commits','Commits'],['recent','Most recent'],['name','Name']]).map(([value,label])=>`<option value="${value}" ${s.sort===value?'selected':''}>${label}</option>`).join('')}</select></label><button class="icon-button" data-action="copy-view" title="Copy this view as Markdown" aria-label="Copy this view as Markdown">⧉</button>${type!=='prs'?`<button class="icon-button" data-action="reverse" data-value="${type}" title="Reverse sort" aria-label="Reverse sort">${s.reverse?'↑':'↓'}</button>`:''}</div>`;
}
function renderActivity(total){
 const s=state.activity;const all=currentCommits();const scoped=scopedCommits(all,s.scope);const scopeTotal=totals(scoped);const query=s.query.toLowerCase();
 let rows=sortRows(activityRows(all,visibleRepositories(),s.scope),s.sort,s.reverse).filter(r=>r.label.toLowerCase().includes(query)||(!s.scope.repo&&r.id.toLowerCase().includes(query)));
 const selected=rows.find(r=>r.id===s.selected)||rows[0];if(selected)s.selected=selected.id;
 const inspectScope=s.scope.file?s.scope:selected?(s.scope.repo?{repo:s.scope.repo,path:selected.path}:{repo:selected.id,path:''}):s.scope;
 const inspectRecords=scopedCommits(all,inspectScope);
 const paths=s.scope.path.split('/').filter(Boolean);
 const breadcrumbs=`<div class="breadcrumbs"><button data-action="activity-root">Repositories</button>${s.scope.repo?`${icon('chevron')}<button data-action="activity-path" data-value="">${esc(s.scope.repo)}</button>`:''}${paths.map((p,i)=>`${icon('chevron')}<button data-action="activity-path" data-value="${esc(paths.slice(0,i+1).join('/'))}" ${i===paths.length-1?'aria-current="location"':''}>${esc(p)}</button>`).join('')}</div>`;
 return `<section class="board-summary"><div class="summary-main"><span class="metric-label">LINES CHANGED</span><strong>${compact(scopeTotal.added+scopeTotal.removed)}${scopeTotal.unknown?'<small> + unknown</small>':''}</strong>${delta(scopeTotal.added,scopeTotal.removed)}</div><div><span class="metric-label">COMMITS</span><strong>${number(scopeTotal.commits)}</strong></div><div><span class="metric-label">CONTRIBUTORS</span><strong>${scopeTotal.people}</strong></div><div class="summary-legend"><span><i class="legend-add"></i> Added</span><span><i class="legend-remove"></i> Removed</span><small>Default-branch history</small></div></section>${filterToolbar('activity')}<div class="board-split"><section class="board-panel activity-panel">${breadcrumbs}<div class="panel-heading"><h2>${esc(scopeTitle())}</h2><span>${s.scope.file?`${scoped.length} commits`:`${rows.length} ${s.scope.repo?'entries':'repositories'}`}</span></div>${s.scope.file?renderCommits(scoped.filter(c=>`${c.subject} ${c.author}`.toLowerCase().includes(query)),true):`<div class="activity-table">${tableHead(['NAME / CHANGE VOLUME','COMMITS','PEOPLE','LATEST',''])}<div class="table-body">${rows.length?rows.map(row=>`<div class="data-row ${selected?.id===row.id?'selected':''}" tabindex="0" data-row="activity" data-id="${esc(row.id)}" data-kind="${row.kind}" aria-label="${esc(row.label)}, ${number(row.added+row.removed)} lines changed"><div class="row-main"><div class="row-title"><span class="row-icon">${icon(row.kind==='repository'?'repo':row.kind==='directory'?'folder':'file')}</span><strong>${esc(row.label)}</strong>${row.stale?'<span class="badge pending">Stale</span>':''}</div>${!s.scope.repo?`<span class="row-subtitle">${esc(row.id.split('/')[0])}</span>`:''}${bar(row.added,row.removed,Math.max(1,...rows.map(r=>r.added+r.removed)))}${delta(row.added,row.removed,row.unknown)}</div><span class="cell-number">${number(row.commits)}</span><span class="cell-number">${row.people}</span><span class="cell-date">${row.latest?age(row.latest):'Quiet'}</span><button class="row-open" data-action="explore" data-value="${esc(row.id)}" data-kind="${row.kind}" aria-label="Explore ${esc(row.label)}">›</button></div>`).join(''):empty('No activity matches','Try a different search, date range, or repository selection.')}</div></div>`}</section><aside class="inspector">${renderInspector(inspectRecords,inspectScope,selected)}</aside></div>`;
}
function renderInspector(records,scope,selected){
 const top=people(records);const total=totals(records);const related=filterPRs(state.snapshot,{days:state.days,repos:state.repos}).filter(p=>p.repo===scope.repo&&(!scope.path||(p.files||[]).some(f=>f===scope.path||f.startsWith(scope.path+'/'))));
 const title=scope.path?scope.path.split('/').pop():scope.repo?.split('/').pop()||'Selection';
 return `<div class="inspector-eyebrow">IN FOCUS</div><h2>${esc(title)}</h2><p class="inspector-path">${esc(scope.path||scope.repo||'Select a repository to inspect its activity.')}</p><div class="inspector-total">${delta(total.added,total.removed,total.unknown)}<span>${number(total.commits)} commits</span></div><div class="section-heading"><h3>Contributors</h3><span>${top.length}</span></div>${top.length?`<div class="person-list">${top.slice(0,8).map((p,i)=>`<button class="person-mini" data-action="person" data-value="${esc(p.id)}"><span class="avatar color-${i%5}">${esc(initials(p.name))}</span><span class="person-info"><strong>${esc(p.name)}</strong><span>${p.commits} commits${p.bot?' · bot':''}</span></span><span class="person-volume">${compact(p.added+p.removed)}</span></button>`).join('')}</div>`:'<p class="muted-text">No contributors in this period.</p>'}<div class="section-heading"><h3>Recent activity</h3><span>30 days</span></div>${renderTimeline(records,30,true)}<div class="section-heading"><h3>Open pull requests</h3><span>${related.length}</span></div>${related.length?related.slice(0,3).map(p=>`<button class="pr-mini" data-action="pr" data-value="${esc(p.repo+'#'+p.number)}"><span class="pr-mini-number">#${p.number}</span><strong>${esc(p.title)}</strong><span>${esc(p.author)} · ${age(p.updatedAt)}</span></button>`).join(''):`<p class="muted-text">${state.snapshot.mode==='cache'?'PR context is unavailable offline.':'No matching PRs in this snapshot.'}</p>`}${scope.repo?`<button class="button wide subtle" data-action="open-repo" data-value="${esc(scope.repo)}">View repository ${icon('link')}</button>`:''}`;
}
function renderCommits(records,showFiles=false){
 const sorted=uniqueCommits(records).sort((a,b)=>dateValue(b.date)-dateValue(a.date));
 return sorted.length?`<div class="commit-list">${sorted.map(c=>`<article class="commit"><span class="commit-node"></span><div><button class="commit-title" data-action="open-commit" data-repo="${esc(c.repo)}" data-value="${esc(c.id)}">${esc(c.subject||'Untitled commit')}</button><p>${esc(c.author)} <span>· ${age(c.date)} · <code>${esc(c.id.slice(0,7))}</code></span>${c.ai?' <span class="badge muted">AI assisted</span>':''}</p>${showFiles?`<div class="commit-files">${(c.files||[]).map(f=>`<span>${esc(f.path)}</span>`).join('')}</div>`:''}</div>${delta(c.added,c.removed,c.linesUnknown)}</article>`).join('')}</div>`:empty('No commits in this view','Expand the date range to explore more history.');
}
function renderTimeline(records,days=30,small=false){
 const series=dailySeries(records,days);const max=Math.max(1,...series.map(p=>p.commits));
 return `<div class="timeline ${small?'small':''}" role="img" aria-label="Commit activity over the last ${series.length} days">${series.map(p=>`<div class="timeline-column" title="${esc(new Date(p.date).toLocaleDateString())}: ${p.commits} commits, +${p.added} −${p.removed}"><span style="height:${p.commits?Math.max(5,p.commits/max*100):2}%" class="${p.commits?'':'quiet'}"></span></div>`).join('')}</div><div class="chart-axis"><span>${new Date(series[0].date).toLocaleDateString(undefined,{month:'short',day:'numeric'})}</span><span>Today</span></div>`;
}
function renderPeople(total){
 const s=state.people;const records=currentCommits();const list=sortRows(people(records).filter(p=>`${p.name} ${p.email}`.toLowerCase().includes(s.query.toLowerCase())),s.sort,s.reverse);const selected=list.find(p=>p.id===s.selected)||list[0];if(selected)s.selected=selected.id;
 const selectedRecords=selected?records.filter(c=>identity(c)===selected.id):[];const byRepo=selected?activityRows(selectedRecords,visibleRepositories(),{}).filter(r=>r.commits).sort((a,b)=>(b.added+b.removed)-(a.added+a.removed)):[];
 const days=state.days||365;const heat=dailySeries(selectedRecords,Math.min(days,91));const max=Math.max(1,...heat.map(p=>p.commits));
 return `<section class="board-summary people-summary"><div class="summary-main"><span class="metric-label">CONTRIBUTORS</span><strong>${list.length}</strong></div><div><span class="metric-label">COMMITS</span><strong>${number(total.commits)}</strong></div><div><span class="metric-label">AI ASSISTED COMMITS</span><strong>${total.commits?Math.round(total.ai/total.commits*100):0}<small>%</small></strong></div><p class="summary-description">Explore contribution patterns<br>across repositories and time.</p></section>${filterToolbar('people')}<div class="board-split people-split"><section class="board-panel"><div class="panel-heading"><h2>Contributors</h2><span>${list.length} in scope</span></div><div class="contributor-list">${list.length?list.map((p,i)=>`<button class="contributor-row ${selected?.id===p.id?'selected':''}" data-action="select-person" data-value="${esc(p.id)}"><span class="avatar color-${i%5}">${esc(initials(p.name))}</span><span class="contributor-name"><strong>${esc(p.name)}${p.bot?'<span class="badge muted">BOT</span>':''}</strong><span>${p.commits} commits · ${p.repos.length} repositories</span>${bar(p.added,p.removed,Math.max(1,...list.map(r=>r.added+r.removed)))}</span><span class="contributor-change">${compact(p.added+p.removed)}<small>lines</small></span></button>`).join(''):empty('No contributors match','Try another name or include bot contributions.')}</div></section><section class="board-panel contributor-detail">${selected?`<div class="detail-person"><span class="avatar large">${esc(initials(selected.name))}</span><div><div class="eyebrow">CONTRIBUTOR</div><h2>${esc(selected.name)}</h2><p>${esc(selected.email||'No email recorded')}</p></div></div><div class="person-stats"><div><strong>${number(selected.commits)}</strong><span>commits</span></div><div><strong>${selected.repos.length}</strong><span>repositories</span></div><div><strong>${new Set(selectedRecords.map(c=>new Date(c.date).toDateString())).size}</strong><span>active days</span></div><div>${delta(selected.added,selected.removed,selected.unknown)}<span>lines changed</span></div></div><div class="section-heading"><h3>Contribution rhythm</h3><span>${Math.min(days,365)} days</span></div>${renderTimeline(selectedRecords,days)}<div class="heatmap" role="img" aria-label="Daily commit heatmap">${heat.map(p=>`<span class="heat-cell level-${p.commits?Math.max(1,Math.ceil(p.commits/max*4)):0}" title="${esc(new Date(p.date).toLocaleDateString())}: ${p.commits} commits"></span>`).join('')}</div><div class="section-heading"><h3>Where the work happened</h3><span>${byRepo.length} repositories</span></div><div class="repo-breakdown">${byRepo.map(r=>`<button data-action="activity-repo" data-value="${esc(r.id)}"><span>${esc(r.label)}</span>${bar(r.added,r.removed,Math.max(1,...byRepo.map(x=>x.added+x.removed)))}<strong>${compact(r.added+r.removed)}</strong></button>`).join('')}</div><div class="section-heading"><h3>Recent commits</h3><span>${selected.commits}</span></div>${renderCommits(selectedRecords.slice().sort((a,b)=>dateValue(b.date)-dateValue(a.date)).slice(0,8))}`:empty('Choose a contributor','Select a person to explore their work.')}</section></div>`;
}
function prSelect(field,label,options){const s=state.prs;return `<label><span class="sr-only">${label}</span><select data-field="${field}" data-board="prs" aria-label="${label}"><option value="">${label}</option>${options.map(([v,t])=>`<option value="${esc(v)}" ${s[field]===v?'selected':''}>${esc(t)}</option>`).join('')}</select></label>`;}
function renderPRs(){
 const s=state.prs;const list=filterPRs(state.snapshot,{...s,days:state.days,repos:state.repos});const selected=list.find(p=>p.repo+'#'+p.number===s.selected)||list[0];if(selected)s.selected=selected.repo+'#'+selected.number;
 const authors=[...new Set(state.snapshot.pullRequests.map(p=>p.author))].sort();const attention=filterPRs(state.snapshot,{days:state.days,repos:state.repos}).filter(p=>(p.reviewers||[]).includes(state.snapshot.viewer)&&state.snapshot.viewer).length;
 return `<div class="pr-context"><div><span class="metric-label">OPEN IN THIS VIEW</span><strong>${list.length}</strong></div><div><span class="metric-label">AWAITING YOUR REVIEW</span><strong>${state.snapshot.viewer?attention:'—'}</strong></div><p>${state.snapshot.viewer?`Signed in as <strong>${esc(state.snapshot.viewer)}</strong>`:'Reviewer matching requires a GitHub account.'}<br><span>Checks and reviews are reported evidence, not merge readiness.</span></p></div>${filterToolbar('prs')}<div class="pr-filters">${prSelect('mine','Everyone',[['authored','Authored by me'],['requested','My review requested']])}${prSelect('author','All authors',authors.map(a=>[a,a]))}${prSelect('review','Any review',[['REVIEW_REQUIRED','Needs review'],['APPROVED','Approved'],['CHANGES_REQUESTED','Changes requested'],['UNKNOWN','Unknown']])}${prSelect('checks','Any checks',[['SUCCESS','Passing'],['FAILURE','Failing'],['PENDING','Pending'],['UNKNOWN','Unknown']])}${prSelect('state','Any state',[['open','Ready for review'],['draft','Draft']])}<button class="text-button" data-action="reset-pr">Reset filters</button></div><div class="board-split pr-split"><section class="board-panel pr-list"><div class="panel-heading"><h2>Pull requests</h2><span>${list.length} results</span></div>${list.length?list.map(p=>`<button class="pr-row ${selected?.repo===p.repo&&selected?.number===p.number?'selected':''}" data-action="select-pr" data-value="${esc(p.repo+'#'+p.number)}"><span class="pr-state-icon ${p.draft?'draft':''}">${p.draft?'◇':'⑂'}</span><span class="pr-row-content"><span class="pr-row-meta">${esc(p.repo)} <strong>#${p.number}</strong>${p.draft?'<span class="badge muted">Draft</span>':''}</span><strong class="pr-title">${esc(p.title)}</strong><span class="pr-row-footer"><span>${esc(p.author)} · ${age(p.updatedAt)}</span><span>${badge(p.review)}${badge(p.checks)}</span></span></span></button>`).join(''):state.snapshot.mode==='cache'?empty('PR context is unavailable offline','Choose GitHub in workspace settings to load open pull requests.'):empty('No pull requests match','Adjust your filters or refresh to check for new work.')}</section><aside class="inspector pr-inspector">${selected?renderPRDetail(selected):'<p class="muted-text">Select a pull request to inspect its context.</p>'}</aside></div>`;
}
function renderPRDetail(p){
 return `<div class="inspector-eyebrow">PULL REQUEST <span>#${p.number}</span></div><h2>${esc(p.title)}</h2><p class="inspector-path">${esc(p.repo)}</p><button class="button primary wide" data-action="open-url" data-value="${esc(p.url)}" ${safeURL(p.url)?'':'disabled'}>Open on GitHub ${icon('link')}</button><dl class="pr-facts"><div><dt>Author</dt><dd>${esc(p.author)}</dd></div><div><dt>State</dt><dd>${p.draft?'Draft':'Open'}</dd></div><div><dt>Review</dt><dd>${badge(p.review)}</dd></div><div><dt>Checks</dt><dd>${badge(p.checks)}</dd></div><div><dt>Updated</dt><dd title="${esc(fullDate(p.updatedAt))}">${age(p.updatedAt)}</dd></div><div><dt>Reviewers</dt><dd>${esc((p.reviewers||[]).join(', ')||'None requested')}</dd></div></dl>${!p.complete?'<p class="coverage-note">Some supporting context is unavailable. Open GitHub for the complete record.</p>':''}<div class="section-heading"><h3>Changed files</h3><span>${p.files?.length||0}</span></div><div class="file-list">${p.files?.length?p.files.map(f=>`<span>${icon('file')}${esc(f)}</span>`).join(''):'<p class="muted-text">No file evidence available.</p>'}</div>`;
}
function openModal(type){
 lastFocus=document.activeElement;state.modal=type;state.dialogError='';
 if(type==='settings'){state.settingsDraft=structuredClone(state.snapshot.settings);state.discovered=[...state.snapshot.repositories];}
 if(type==='repositories')state.repoDraft=[...state.repos];
 renderModal();
}
function closeModal(){state.modal=null;$('#modal-root').innerHTML='';if(lastFocus?.isConnected)lastFocus.focus();}
function renderModal(){
 if(!state.modal){$('#modal-root').innerHTML='';return;}
 const titles={settings:'Workspace settings',repositories:'Repository scope',status:'Data status',help:'Keyboard guide'};
 let content='';let footer='<button class="button" data-action="close-modal">Done</button>';
 if(state.modal==='settings'){
 const draft=state.settingsDraft;
 content=`<p class="modal-intro">Choose the data behind your board. Desktop preferences are saved separately from your terminal settings.</p><div class="mode-options" role="group" aria-label="Data source">${[['demo','Demo','Explore a synthetic workspace'],['cache','Cached history','Read your existing local cache'],['live','GitHub','Refresh repositories and PRs']].map(([mode,title,description])=>`<button data-action="settings-mode" data-value="${mode}" class="mode-option ${draft.mode===mode?'selected':''}" aria-pressed="${draft.mode===mode}"><strong>${title}</strong><span>${description}</span></button>`).join('')}</div>${draft.mode!=='demo'?`<div class="settings-fields"><label>Personal account<input id="personalAccount" name="personalAccount" autocomplete="off" value="${esc(draft.personalAccount)}" placeholder="GitHub username"></label><label>Organizations<input id="organizations" name="organizations" autocomplete="off" value="${esc((draft.organizations||[]).join(', '))}" placeholder="Separate names with commas"></label></div><p class="field-help">${draft.mode==='live'?'Uses your signed-in GitHub CLI. Git and gh must be installed. Refresh reads default-branch history and open PRs.':'Reads owned Bigboard caches without network requests. Uncached repositories will have no history.'}</p><div class="section-heading"><h3>Tracked repositories</h3><button class="button small" data-action="discover" ${state.busy?'disabled':''}>${state.busy?'Working…':'Discover repositories'}</button></div><p class="field-help">Select repositories to include. With none selected, all discovered repositories are included.</p><div class="repository-choices">${state.discovered.length?state.discovered.map(r=>`<label><input type="checkbox" data-settings-repo="${esc(r.id)}" ${(draft.repositories||[]).includes(r.id)?'checked':''}><span>${esc(r.id)}</span></label>`).join(''):'<p class="muted-text">Discover repositories after entering your account or organizations.</p>'}</div>`:'<div class="demo-explainer"><span class="demo-label">DEMO</span><p>Sample repositories, contributors, and pull requests let you explore every board. Nothing is fetched from GitHub.</p></div>'}`;
 footer=`<button class="button" data-action="close-modal">Cancel</button><button class="button primary" data-action="save-settings" ${state.busy?'disabled':''}>${state.busy?'Saving…':'Save workspace'}</button>`;
 }else if(state.modal==='repositories'){
 content=`<p class="modal-intro">This viewing scope applies to all three boards. It does not change which repositories are refreshed.</p><div class="scope-actions"><button class="text-button" data-action="scope-all">All repositories</button><span>${state.repoDraft.length?state.repoDraft.length+' selected':'All included'}</span></div><div class="repository-choices">${state.snapshot.repositories.map(r=>`<label><input type="checkbox" data-scope-repo="${esc(r.id)}" ${state.repoDraft.includes(r.id)?'checked':''}><span>${esc(r.id)}</span></label>`).join('')}</div>`;
 footer='<button class="button" data-action="close-modal">Cancel</button><button class="button primary" data-action="apply-scope">Apply scope</button>';
 }else if(state.modal==='status'){
 const snap=state.snapshot;
 content=`<div class="status-overview"><span class="status-dot ${snap.status.errors?.length?'warning':''}"></span><div><h3>${snap.mode==='demo'?'Synthetic demo data':snap.status.errors?.length?'Some data needs attention':'Snapshot available'}</h3><p>${esc(snap.status.message||'Your current snapshot is ready to explore.')}</p></div></div><dl class="status-facts"><div><dt>Source</dt><dd>${esc({demo:'Demo workspace',cache:'Offline repository cache',live:'GitHub and local Git history'}[snap.mode])}</dd></div><div><dt>Snapshot</dt><dd>${esc(fullDate(snap.generatedAt))}</dd></div><div><dt>Coverage</dt><dd>${snap.repositories.length} repositories · ${number(snap.commits.length)} commit records · ${snap.pullRequests.length} PRs</dd></div><div><dt>Cache location</dt><dd>${esc(snap.status.cachePath||'Not used in demo mode')}</dd></div></dl>${snap.status.errors?.length?`<h3 class="status-heading">Diagnostics</h3><ul class="diagnostics">${snap.status.errors.map(e=>`<li>${esc(e)}</li>`).join('')}</ul>`:''}<h3 class="status-heading">Repository coverage</h3><div class="coverage-list">${snap.repositories.map(r=>`<div><span>${esc(r.id)}<small>${esc(r.branch||'Default branch')}${r.revision?' · '+esc(r.revision.slice(0,7)):''}</small></span><span class="badge ${r.stale?'pending':'muted'}">${r.stale?'Stale':'Available'}</span>${r.error?`<p>${esc(r.error)}</p>`:''}</div>`).join('')}</div><p class="field-help">The date and repository filters only change your view. Refresh runs when you request it; background polling is not enabled.</p>`;
 }else{
 content=`<p class="modal-intro">Move between boards without losing your filters or position. Shortcuts stay out of the way while you type.</p><div class="shortcut-list">${[['a','Activity board'],['c','Contributors board'],['p','Pull requests board'],['/','Search the current board'],['r','Refresh workspace'],['y','Copy this view as Markdown'],[',','Workspace settings'],['?','Keyboard guide'],['Esc','Close dialog or go up one directory'],['Enter','Explore a focused activity row'],['↑ ↓','Move between activity rows'],['Tab','Move between controls']].map(([key,description])=>`<div><kbd>${key}</kbd><span>${description}</span></div>`).join('')}</div><p class="field-help">Click a repository or directory to inspect its contributors. Use the arrow at the end of the row, double-click, or press Enter to explore it.</p>`;
 }
 const old=$('#modal-root dialog');const focusID=document.activeElement?.id;
 $('#modal-root').innerHTML=`<dialog class="modal ${state.modal==='settings'?'settings-modal':''}" aria-labelledby="modal-title"><header><div class="eyebrow">BIGBOARD</div><h2 id="modal-title">${titles[state.modal]}</h2><button class="icon-button modal-close" data-action="close-modal" aria-label="Close dialog">×</button></header><div class="modal-body">${state.dialogError?`<div class="dialog-error" role="alert">${esc(state.dialogError)}</div>`:''}${content}</div><footer>${footer}</footer></dialog>`;
 const dialog=$('#modal-root dialog');dialog.showModal();dialog.addEventListener('cancel',e=>{e.preventDefault();closeModal();});
 if(old&&focusID)document.getElementById(focusID)?.focus();
}
function readSettings(){
 if($('#personalAccount'))state.settingsDraft.personalAccount=$('#personalAccount').value.trim();
 if($('#organizations'))state.settingsDraft.organizations=$('#organizations').value.split(',').map(s=>s.trim()).filter(Boolean);
 return state.settingsDraft;
}
async function refresh(){
 if(state.busy)return;state.busy=true;state.error='';render();
 try{state.snapshot=validSnapshot(await api('Refresh'));toast('Workspace refreshed');}catch(e){state.error=e.message;}finally{state.busy=false;render();}
}

async function copyView(){
 const cell=value=>String(value??'').replaceAll('|','\\|').replaceAll('\n',' ');
 let heading,columns,rows;
 if(state.board==='prs'){
  heading='Pull requests';columns=['Repository','PR','Title','Author','Review','Checks'];rows=filterPRs(state.snapshot,{...state.prs,days:state.days,repos:state.repos}).map(p=>[p.repo,'#'+p.number,p.title,p.author,statusName(p.review),statusName(p.checks)]);
 }else if(state.board==='people'){
  heading='Contributors';columns=['Contributor','Commits','Added','Removed','Repositories'];rows=sortRows(people(currentCommits()).filter(p=>`${p.name} ${p.email}`.toLowerCase().includes(state.people.query.toLowerCase())),state.people.sort,state.people.reverse).map(p=>[p.name,p.commits,p.added,p.removed,p.repos.length]);
 }else{
  heading=scopeTitle();
  if(state.activity.scope.file){columns=['Commit','Subject','Author','Added','Removed'];rows=uniqueCommits(scopedCommits(currentCommits(),state.activity.scope)).filter(c=>`${c.subject} ${c.author}`.toLowerCase().includes(state.activity.query.toLowerCase())).map(c=>[c.id.slice(0,7),c.subject,c.author,c.added,c.removed]);}
  else{columns=['Name','Commits','Added','Removed','Contributors'];rows=sortRows(activityRows(currentCommits(),visibleRepositories(),state.activity.scope),state.activity.sort,state.activity.reverse).filter(r=>`${r.label} ${r.id}`.toLowerCase().includes(state.activity.query.toLowerCase())).map(r=>[r.id,r.commits,r.added,r.removed,r.people]);}
 }
 const text=`## ${heading}\n\n${state.snapshot.mode==='demo'?'Synthetic demo data · ':''}${state.days?`Last ${state.days} days`:'All time'}\n\n| ${columns.join(' | ')} |\n| ${columns.map(()=>'---').join(' | ')} |\n${rows.map(row=>'| '+row.map(cell).join(' | ')+' |').join('\n')}`;
 if(window.go?.main?.App?.CopyText)await window.go.main.App.CopyText(text);else await navigator.clipboard.writeText(text);
 toast('Copied this view as Markdown');
}

async function openURL(value){
 if(state.snapshot.mode==='demo'){toast('Demo repositories are synthetic. Connect a workspace to open real evidence.');return;}
 const url=safeURL(value);if(!url){toast('This GitHub link is unavailable.');return;}
 try{if(window.go?.main?.App?.OpenURL)await window.go.main.App.OpenURL(url);else window.open(url,'_blank','noopener,noreferrer');}catch(e){toast(e.message);}
}
function explore(id,kind){
 const s=state.activity;
 if(kind==='repository')s.scope={repo:id,path:'',file:false};else s.scope={...s.scope,path:id,file:kind==='file'};
 s.query='';s.selected='';render();
}
function back(){const s=state.activity;if(!s.scope.repo)return;const path=s.scope.path.split('/').filter(Boolean);if(path.length){path.pop();s.scope={...s.scope,path:path.join('/'),file:false};}else s.scope={repo:'',path:'',file:false};s.query='';s.selected='';render();}
async function action(button){
 const value=button.dataset.value;
 switch(button.dataset.action){
 case 'board':state.board=value;render();break;
 case 'range':state.days=Number(value);render();break;
 case 'refresh':await refresh();break;
 case 'copy-view':await copyView();break;
 case 'dismiss-error':state.error='';render();break;
 case 'settings':case 'status':case 'help':case 'repositories':openModal(button.dataset.action);break;
 case 'close-modal':closeModal();break;
 case 'reverse':state[value].reverse=!state[value].reverse;render();break;
 case 'activity-root':state.activity.scope={repo:'',path:'',file:false};state.activity.query='';state.activity.selected='';render();break;
 case 'activity-path':state.activity.scope={...state.activity.scope,path:value,file:state.activity.scope.file&&state.activity.scope.path===value};state.activity.query='';state.activity.selected='';render();break;
 case 'explore':explore(value,button.dataset.kind);break;
 case 'activity-repo':state.board='activity';explore(value,'repository');break;
 case 'person':state.board='people';state.people.query='';state.people.selected=value;render();break;
 case 'select-person':state.people.selected=value;render();break;
 case 'pr':state.board='prs';state.prs={...state.prs,query:'',author:'',review:'',checks:'',state:'',mine:'',selected:value};render();break;
 case 'select-pr':state.prs.selected=value;render();break;
 case 'open-url':await openURL(value);break;
 case 'open-repo':await openURL(`https://github.com/${value}`);break;
 case 'open-commit':await openURL(`https://github.com/${button.dataset.repo}/commit/${value}`);break;
 case 'reset-pr':state.prs={...state.prs,query:'',author:'',review:'',checks:'',state:'',mine:''};render();break;
 case 'settings-mode':readSettings();state.settingsDraft.mode=value;renderModal();break;
 case 'scope-all':state.repoDraft=[];renderModal();break;
 case 'apply-scope':state.repos=[...state.repoDraft];if(state.repos.length&&state.activity.scope.repo&&!state.repos.includes(state.activity.scope.repo))state.activity.scope={repo:'',path:'',file:false};closeModal();render();break;
 case 'discover':{
  readSettings();state.busy=true;state.dialogError='';renderModal();
  try{state.discovered=(await api('Discover',state.settingsDraft))||[];if(!state.discovered.length)state.dialogError='No repositories were found for these sources.';}catch(e){state.dialogError=e.message;}finally{state.busy=false;renderModal();}break;
 }
 case 'save-settings':{
  readSettings();state.busy=true;state.dialogError='';renderModal();
  try{state.snapshot=validSnapshot(await api('SaveSettings',state.settingsDraft));state.repos=[];state.activity.scope={repo:'',path:'',file:false};state.activity.selected='';state.people.selected='';state.prs.selected='';closeModal();toast('Workspace saved');}catch(e){state.dialogError=e.message;}finally{state.busy=false;render();if(state.modal)renderModal();}break;
 }
 }
}
document.addEventListener('click',event=>{
 const button=event.target.closest('[data-action]');if(button){action(button).catch(e=>toast(e.message));return;}
 const row=event.target.closest('[data-row="activity"]');if(row){state.activity.selected=row.dataset.id;render();const current=[...document.querySelectorAll('[data-row="activity"]')].find(r=>r.dataset.id===row.dataset.id);current?.focus({preventScroll:true});}
});
document.addEventListener('dblclick',event=>{const row=event.target.closest('[data-row="activity"]');if(row)explore(row.dataset.id,row.dataset.kind);});
document.addEventListener('input',event=>{
 const input=event.target;
 if(input.dataset.field==='query'){
  const start=input.selectionStart;state[input.dataset.board].query=input.value;render();const next=document.querySelector(`[data-board="${input.dataset.board}"][data-field="query"]`);next.focus();next.setSelectionRange(start,start);
 }
});
document.addEventListener('change',event=>{
 const input=event.target;
 if(input.dataset.field){state[input.dataset.board][input.dataset.field]=input.type==='checkbox'?input.checked:input.value;render();}
 if(input.dataset.settingsRepo){const repos=new Set(state.settingsDraft.repositories||[]);input.checked?repos.add(input.dataset.settingsRepo):repos.delete(input.dataset.settingsRepo);state.settingsDraft.repositories=[...repos];}
 if(input.dataset.scopeRepo){const repos=new Set(state.repoDraft);input.checked?repos.add(input.dataset.scopeRepo):repos.delete(input.dataset.scopeRepo);state.repoDraft=[...repos];}
});
document.addEventListener('keydown',event=>{
 if(state.modal)return;
 if(event.ctrlKey||event.metaKey||event.altKey||['INPUT','TEXTAREA','SELECT'].includes(event.target.tagName)||event.target.isContentEditable)return;
 const boards={a:'activity',c:'people',p:'prs'};
 if(boards[event.key]){state.board=boards[event.key];render();event.preventDefault();}
 else if(event.key==='/'){event.preventDefault();$(`[data-board="${state.board}"][data-field="query"]`)?.focus();}
 else if(event.key==='r'){event.preventDefault();refresh();}
 else if(event.key==='y'){event.preventDefault();copyView().catch(e=>toast(e.message));}
 else if(event.key===','){event.preventDefault();openModal('settings');}
 else if(event.key==='?'||event.key==='F1'){event.preventDefault();openModal('help');}
 else if(event.key==='Escape'&&state.board==='activity'){event.preventDefault();back();}
 else if(state.board==='activity'&&['ArrowDown','ArrowUp','Enter'].includes(event.key)){
  const rows=[...document.querySelectorAll('[data-row="activity"]')];const focused=event.target.closest('[data-row="activity"]');
  if(event.key==='Enter'&&focused){event.preventDefault();explore(focused.dataset.id,focused.dataset.kind);return;}
  if(event.key==='Enter')return;
  if(!rows.length)return;event.preventDefault();let index=rows.findIndex(row=>row.dataset.id===state.activity.selected);index=Math.max(0,Math.min(rows.length-1,index+(event.key==='ArrowDown'?1:-1)));state.activity.selected=rows[index].dataset.id;render();const next=[...document.querySelectorAll('[data-row="activity"]')][index];next.focus();next.scrollIntoView({block:'nearest'});
 }
});
async function start(){
 try{state.snapshot=validSnapshot(await api('Snapshot'));render();if(state.snapshot.mode!=='demo'&&(!dateValue(state.snapshot.generatedAt)||state.snapshot.status.phase==='initial'))await refresh();}
 catch(e){$('#app').innerHTML=`<div class="boot"><div class="brand-mark">▥</div><h1>Unable to open the workspace</h1><p>${esc(e.message)}</p><button class="button primary" id="retry-start">Try again</button></div>`;$('#retry-start').addEventListener('click',start);}
}
start();
