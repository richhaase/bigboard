import {ranges,number,compact,identity,change,dateValue,selectCommits,uniqueCommits,totals,people,scopedCommits,activityRows,sortRows,dailySeries,filterPRs,safeURL,facetValues,updatedSinceReview,needsAttention,timelineSeries,relatedScopes} from './model.js';

const $=selector=>document.querySelector(selector);
const esc=value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const icons={activity:'▥',people:'◉',prs:'⑂',folder:'▰',file:'≡',repo:'▱',refresh:'↻',settings:'⚙',chevron:'›',back:'←',close:'×',link:'↗',search:'⌕'};
const icon=name=>`<span aria-hidden="true">${icons[name]||name}</span>`;
const api=async(method,arg)=>{
  if(window.go?.main?.App?.[method]) return window.go.main.App[method](...(arg===undefined?[]:[arg]));
  const paths={Snapshot:'snapshot',Refresh:'refresh',SaveSettings:'settings',Discover:'discover',IdentityOptions:'identities',SaveIdentity:'identity',RepositoryControls:'repository-controls',ApplyRepositoryControls:'repository-controls',RemoveCachedRepository:'remove-cache',SelectGroup:'group',OpenPath:'path',ExportJSON:'export'};
  const response=await fetch(`/api/${paths[method]}`,{method:['Snapshot','IdentityOptions','RepositoryControls','ExportJSON'].includes(method)?'GET':'POST',headers:{'Content-Type':'application/json'},...(arg===undefined?{}:{body:JSON.stringify(method==='SelectGroup'?{name:arg}:method==='RemoveCachedRepository'?{id:arg}:arg)})});
  if(!response.ok){let message=await response.text();try{message=JSON.parse(message).error||message;}catch{}throw new Error(message||`Request failed (${response.status})`);}
  return response.json();
};
const state={snapshot:null,board:'activity',days:14,since:'',repos:[],prContext:null,busy:false,error:'',activity:{lens:'ranking',relatedMode:'commits',lensPerson:'',people:[],query:'',sort:'change',reverse:false,hideBots:false,scope:{repo:'',path:'',file:false},selected:''},people:{query:'',sort:'change',reverse:false,hideBots:false,selected:''},prs:{query:'',sort:'updated',reverse:false,author:'',review:'',checks:'',state:'',mine:'',selected:''},modal:null,settingsDraft:null,discovered:[],dialogError:''};
let lastFocus=null;
let preferencesApplied=false;
let renderedBoard='activity';
const boardScroll={activity:0,people:0,prs:0};
const searchBefore={};
const referenceTime=()=>dateValue(state.snapshot?.generatedAt)||Date.now();
const sharedFilters=()=>({days:state.days,repos:state.repos,since:state.since});
const bookmark=()=>state.snapshot.bookmark?.seenAt||'';
const effectiveDays=()=>state.since?Math.max(1,Math.ceil((referenceTime()-dateValue(state.since))/86400000)):state.days;
const rangeLabel=()=>state.since?'Since last look':state.days?`${state.days} days`:'All time';
const afterBookmark=value=>bookmark()&&dateValue(value)>dateValue(bookmark());
const prList=()=>filterPRs(state.snapshot,{...state.prs,...sharedFilters()},referenceTime()).filter(p=>!state.prContext||p.repo===state.prContext.repo);
const mineOptions=[['attention','Needs my attention'],['authored','Authored by me'],['requested','Review requested from me'],['updated','Updated since my review']];
const sortOptions=type=>type==='prs'?[['updated','Updated: newest first'],['repository','Repository: A–Z'],['author','Author: A–Z'],['title','Title: A–Z'],['attention','Attention: oldest first']]:type==='people'?[['change','Lines changed'],['commits','Commits'],['added','Lines added'],['removed','Lines removed'],['net','Net lines'],['ai','AI share'],['name','Name'],['recent','Most recent']]:[['change','Lines changed'],['commits','Commits'],['recent','Most recent'],['name','Name']];
let toastTimer;
let refreshVersion=0;
let prEvidenceLoaded=false;
const prCoverageUnknown=(repo='',path='')=>loadingPRs()||state.snapshot.mode==='cache'||visibleRepositories().filter(r=>!repo||r.id===repo).some(r=>r.prInventoryComplete===false||r.prStale||(path&&r.prFilesComplete===false));
const loadingPRs=()=>state.snapshot.mode==='live'&&state.busy&&!prEvidenceLoaded&&!state.snapshot.pullRequests.length;
let commitLimit=100;
const maximum=(rows,measure)=>rows.reduce((highest,row)=>Math.max(highest,measure(row)),1);
const age=value=>{const ms=referenceTime()-dateValue(value);if(!dateValue(value))return 'Unknown';const days=Math.floor(ms/86400000);return days<0?'Future date':days===0?'Today':days===1?'Yesterday':days<30?`${days}d ago`:new Date(value).toLocaleDateString(undefined,{month:'short',day:'numeric'});};
const fullDate=value=>dateValue(value)?new Date(value).toLocaleString(undefined,{dateStyle:'medium',timeStyle:'short'}):'Unavailable';
const initials=name=>(name||'?').split(/[\s@._-]+/).filter(Boolean).slice(0,2).map(p=>p[0]).join('').toUpperCase();
const statusName=value=>({APPROVED:'Approved',CHANGES_REQUESTED:'Changes requested',REVIEW_REQUIRED:'Needs review',SUCCESS:'Passing',PENDING:'Pending',FAILURE:'Failing',ERROR:'Error',UNKNOWN:'Unknown',EXPECTED:'Expected'}[value]||value?.replaceAll('_',' ').toLowerCase()||'Unknown');
const badge=(value,type='')=>`<span class="badge ${type||(['SUCCESS','APPROVED'].includes(value)?'good':['FAILURE','ERROR','CHANGES_REQUESTED'].includes(value)?'bad':['PENDING','REVIEW_REQUIRED','EXPECTED'].includes(value)?'pending':'muted')}">${esc(statusName(value))}</span>`;
const empty=(title,body)=>`<div class="empty"><span class="empty-symbol">▱</span><h3>${esc(title)}</h3><p>${esc(body)}</p></div>`;
const bar=(added,removed,max=added+removed)=>`<div class="change-bar" role="img" aria-label="${number(added)} lines added, ${number(removed)} removed"><span class="added" style="width:${Math.max(0,Math.min(100,added/Math.max(1,max)*100))}%"></span><span class="removed" style="width:${Math.max(0,Math.min(100,removed/Math.max(1,max)*100))}%"></span></div>`;
const delta=(added,removed,unknown)=>`<span class="delta"><span class="positive">+${compact(added)}</span><span class="negative">−${compact(removed)}</span>${unknown?'<span title="Some line counts are unavailable">?</span>':''}</span>`;
function toast(message){clearTimeout(toastTimer);$('#toast').textContent=message;$('#toast').classList.add('visible');toastTimer=setTimeout(()=>$('#toast').classList.remove('visible'),4500);}
function validSnapshot(data){return {...data,repositories:data.repositories||[],commits:data.commits||[],pullRequests:data.pullRequests||[],settings:{mode:'live',personalAccount:'',organizations:[],repositories:[],...data.settings},status:{phase:'ready',message:'',errors:[],...data.status}};}
function renderEmptyWorkspace(){
 if(state.busy)return empty('Loading your repositories','Existing cached history will appear as soon as it is ready.');
 return `<section class="empty-workspace">${empty('Connect your repositories',state.snapshot.status.message||'Use your existing GitHub account and organizations, or load repositories already cached by Bigboard.')}<button class="button primary" data-action="settings">Open workspace settings</button></section>`;
}
function currentCommits(){return selectCommits(state.snapshot,{...sharedFilters(),hideBots:state[state.board]?.hideBots,people:state.board==='activity'?state.activity.people:[]},referenceTime());}
function visibleRepositories(){return state.snapshot.repositories.filter(r=>!state.repos.length||state.repos.includes(r.id));}
function scopeTitle(){const scope=state.activity.scope;return scope.areaName?scope.areaName:scope.file?scope.path.split('/').pop():scope.path?scope.path.split('/').pop():scope.repo?scope.repo.split('/').pop():'Repository activity';}
function tableHead(columns){return `<div class="table-head">${columns.map(c=>`<span>${c}</span>`).join('')}</div>`;}
function render(){
 const snapshot=state.snapshot;if(!snapshot)return;
 boardScroll[renderedBoard]=document.scrollingElement?.scrollTop||0;
 const configs={activity:['Activity','Follow the change. Find the context.'],people:['Contributors','The people behind the work.'],prs:['Pull requests','See what is moving and what needs attention.']};
 const [title,subtitle]=configs[state.board];const total=totals(currentCommits());
 $('#app').innerHTML=`<aside class="sidebar"><div class="brand"><div class="brand-mark">▥</div><div><strong>BIGBOARD</strong><span>WORK IN VIEW</span></div></div><div class="workspace"><span class="workspace-dot"></span><div><strong>${esc(snapshot.settings.personalAccount||snapshot.settings.organizations?.[0]||'Your workspace')}</strong><span>${snapshot.repositories.length} repositories</span></div></div><div class="nav-label">EXPLORE</div><nav aria-label="Boards">${[['activity','Activity','a'],['people','Contributors','c'],['prs','Pull requests','p']].map(([id,label,key])=>`<button data-action="board" data-value="${id}" aria-label="${label}" class="nav-item ${state.board===id?'active':''}" ${state.board===id?'aria-current="page"':''}>${icon(id)}<span>${label}</span><kbd>${key}</kbd></button>`).join('')}</nav><div class="sidebar-bottom"><button class="nav-item" data-action="controls" aria-label="Repository controls">${icon('repo')}<span>Repository controls</span><kbd>i</kbd></button><button class="nav-item" data-action="status" aria-label="Data status">${icon('◷')}<span>Data status</span><span class="status-dot ${snapshot.status.errors?.length?'warning':''}"></span></button><button class="nav-item" data-action="settings" aria-label="Settings">${icon('settings')}<span>Settings</span><kbd>,</kbd></button><button class="nav-item" data-action="help" aria-label="Keyboard guide">${icon('?')}<span>Keyboard guide</span><kbd>?</kbd></button><div class="app-edition">DESKTOP APP <span>LOCAL FIRST</span></div></div></aside><main class="main"><header class="page-header"><div><div class="eyebrow">YOUR ENGINEERING LANDSCAPE</div><h1>${title}</h1><p>${subtitle}</p></div><button class="button refresh ${state.busy?'spinning':''}" data-action="refresh" ${state.busy?'disabled':''}>${icon('refresh')} ${state.busy?'Refreshing…':'Refresh'}<kbd>r</kbd></button></header><section class="scope-toolbar" aria-label="Shared viewing scope"><div class="date-tabs" role="group" aria-label="Date range">${ranges.map(r=>`<button data-action="range" data-value="${r.value}" class="${!state.since&&state.days===r.value?'selected':''}" aria-pressed="${!state.since&&state.days===r.value}">${r.label}</button>`).join('')}<button data-action="dates" class="${state.since||!ranges.some(r=>r.value===state.days)?'selected':''}">${state.since?'Since last look':!ranges.some(r=>r.value===state.days)?rangeLabel():'Custom…'}</button></div><button class="button scope-button" data-action="repositories">${icon('repo')} ${state.repos.length?`${state.repos.length} selected`:'All repositories'} <span>⌄</span></button>${Object.keys(snapshot.groups||{}).length?`<label class="group-picker"><span class="sr-only">Repository group</span><select data-action="select-group" aria-label="Repository group"><option value="">All groups</option>${Object.keys(snapshot.groups).sort().map(g=>`<option value="${esc(g)}" ${snapshot.activeGroup===g?'selected':''}>${esc(g)}</option>`).join('')}</select></label>`:''}<div class="snapshot-time"><span class="status-dot ${snapshot.status.errors?.length?'warning':''}"></span>${state.busy?'Refreshing data…':snapshot.mode==='cache'?'Cached history':dateValue(snapshot.generatedAt)?`Updated ${age(snapshot.generatedAt).toLowerCase()}`:'Not loaded'}</div></section>${state.error?`<div class="error-banner" role="alert">${esc(state.error)}<button data-action="dismiss-error" aria-label="Dismiss error">×</button></div>`:''}${snapshot.status.errors?.length?`<button class="coverage-banner" data-action="status">${snapshot.status.errors.length} data ${snapshot.status.errors.length===1?'issue':'issues'} · Some results may be incomplete <span>View details →</span></button>`:''}${state.busy?`<div class="loading-banner" role="status"><span class="loading-dot"></span>${esc(snapshot.status.phase==='refreshing'?snapshot.status.message:'Loading repository history…')}</div>`:''}${!snapshot.repositories.length?renderEmptyWorkspace():state.board==='activity'?renderActivity(total):state.board==='people'?renderPeople(total):renderPRs()}<footer class="main-footer"><span>Activity is a signal, not a productivity score.</span><span>${state.board==='prs'?loadingPRs()?'Pull requests loading':number(snapshot.pullRequests.length)+' known open PRs'+(prCoverageUnknown()?' · partial':''):number(total.commits)+' commits in scope'}<span class="footer-separator">/</span>LOCAL WORKSPACE</span></footer></main>`;
 window.scrollTo(0,boardScroll[state.board]||0);renderedBoard=state.board;
}
function filterToolbar(type,extra=''){
 const s=state[type];return `<div class="list-toolbar"><label class="search-field">${icon('search')}<input aria-label="Search ${type==='people'?'contributors':type==='prs'?'pull requests':'activity'}" data-field="query" data-board="${type}" placeholder="${type==='people'?'Find a contributor…':type==='prs'?'Search titles, authors, repositories…':'Find in this view…'}" value="${esc(s.query)}"><kbd>/</kbd></label>${extra}<button class="button small" data-action="filters">Filters${type==='activity'&&state.activity.people.length?' · '+state.activity.people.length:''}</button>${type==='people'?'<button class="button small" data-action="identities">Identities</button><button class="button small" data-action="export-json" title="Export all-time tracked contributor statistics without refreshing">Export JSON</button>':''}${type!=='prs'?`<label class="checkbox-label"><input type="checkbox" data-field="hideBots" data-board="${type}" ${s.hideBots?'checked':''}> Hide bots</label>`:''}<label class="sort-label"><span>Sort</span><select aria-label="Sort ${type}" data-field="sort" data-board="${type}">${sortOptions(type).map(([value,label])=>`<option value="${value}" ${s.sort===value?'selected':''}>${label}</option>`).join('')}</select></label><button class="icon-button" data-action="copy-view" title="Copy this view as Markdown" aria-label="Copy this view as Markdown">⧉</button>${`<button class="icon-button" data-action="reverse" data-value="${type}" title="Reverse sort" aria-label="Reverse sort">${s.reverse?'↑':'↓'}</button>`}</div>`;
}
function renderActivity(total){
 const s=state.activity;const all=currentCommits();const scoped=scopedCommits(all,s.scope);const scopeTotal=totals(scoped);const query=s.query.toLowerCase();
 let rows=sortRows(activityRows(all,visibleRepositories(),s.scope),s.sort,s.reverse).filter(r=>r.label.toLowerCase().includes(query)||(!s.scope.repo&&r.id.toLowerCase().includes(query)));
 const rowMaximum=maximum(rows,r=>r.added+r.removed);
 const selected=rows.find(r=>r.id===s.selected)||rows[0];if(selected)s.selected=selected.id;
 const inspectScope=s.scope.file?s.scope:selected?(s.scope.repo?{repo:s.scope.repo,path:selected.path}:{repo:selected.id,path:''}):s.scope;
 const inspectRecords=scopedCommits(all,inspectScope);
 const paths=s.scope.path.split('/').filter(Boolean);
 const breadcrumbs=`<div class="breadcrumbs"><button data-action="activity-root">Repositories</button>${s.scope.repo?`${icon('chevron')}<button data-action="activity-path" data-value="">${esc(s.scope.repo)}</button>`:''}${paths.map((p,i)=>`${icon('chevron')}<button data-action="activity-path" data-value="${esc(paths.slice(0,i+1).join('/'))}" ${i===paths.length-1?'aria-current="location"':''}>${esc(p)}</button>`).join('')}</div>`;
 return `<section class="board-summary"><div class="summary-main"><span class="metric-label">LINES CHANGED</span><strong>${compact(scopeTotal.added+scopeTotal.removed)}${scopeTotal.unknown?'<small> + unknown</small>':''}</strong>${delta(scopeTotal.added,scopeTotal.removed)}</div><div><span class="metric-label">COMMITS</span><strong>${number(scopeTotal.commits)}</strong></div><div><span class="metric-label">CONTRIBUTORS</span><strong>${scopeTotal.people}</strong></div><div class="summary-legend"><span><i class="legend-add"></i> Added</span><span><i class="legend-remove"></i> Removed</span><small>Default-branch history</small></div></section>${filterToolbar('activity')}<div class="board-split"><section class="board-panel activity-panel">${breadcrumbs}${renderLensTabs()}<div class="panel-heading"><h2>${esc(scopeTitle())}</h2><span>${s.scope.file?`${scoped.length} commits`:`${rows.length} ${s.scope.repo?'entries':'repositories'}`}</span></div>${s.lens!=='ranking'?renderLens(scoped):s.scope.file?renderCommits(scoped.filter(c=>`${c.subject} ${c.author}`.toLowerCase().includes(query)),true):`<div class="activity-table">${tableHead(['NAME / CHANGE VOLUME','COMMITS','PEOPLE','LATEST',''])}<div class="table-body">${rows.length?rows.map(row=>`<div class="data-row ${selected?.id===row.id?'selected':''}" tabindex="0" data-row="activity" data-id="${esc(row.id)}" data-kind="${row.kind}" aria-label="${esc(row.label)}, ${number(row.added+row.removed)} lines changed"><div class="row-main"><div class="row-title"><span class="row-icon">${icon(row.kind==='repository'?'repo':row.kind==='directory'?'folder':'file')}</span><strong>${esc(row.label)}</strong>${row.stale?'<span class="badge pending">Stale</span>':''}</div>${!s.scope.repo?`<span class="row-subtitle">${esc(row.id.split('/')[0])}</span>`:''}${bar(row.added,row.removed,rowMaximum)}${delta(row.added,row.removed,row.unknown)}${renderRowPRs(row)}</div><span class="cell-number">${number(row.commits)}</span><span class="cell-number">${row.people}</span><span class="cell-date">${row.latest?age(row.latest):'Quiet'}${afterBookmark(row.latest)?'<span class="bookmark-marker" title="Updated since last look">+</span>':''}</span><button class="row-open" data-action="explore" data-value="${esc(row.id)}" data-kind="${row.kind}" aria-label="Explore ${esc(row.label)}">›</button></div>`).join(''):empty('No activity matches','Try a different search, date range, or repository selection.')}</div></div>`}</section><aside class="inspector">${renderInspector(inspectRecords,inspectScope,selected)}</aside></div>`;
}
function renderInspector(records,scope,selected){
 const top=people(records);const total=totals(records);const related=filterPRs(state.snapshot,sharedFilters(),referenceTime()).filter(p=>p.repo===scope.repo&&(!scope.path||(p.files||[]).some(f=>f===scope.path||f.startsWith(scope.path+'/'))));
 const title=scope.path?scope.path.split('/').pop():scope.repo?.split('/').pop()||'Selection';
 return `<div class="inspector-eyebrow">IN FOCUS</div><h2>${esc(title)}</h2><p class="inspector-path">${esc(scope.path||scope.repo||'Select a repository to inspect its activity.')}</p><div class="inspector-total">${delta(total.added,total.removed,total.unknown)}<span>${number(total.commits)} commits</span></div><div class="section-heading"><h3>Contributors</h3><span>${top.length}</span></div>${top.length?`<div class="person-list">${top.slice(0,8).map((p,i)=>`<button class="person-mini" data-action="person" data-value="${esc(p.id)}"><span class="avatar color-${i%5}">${esc(initials(p.name))}</span><span class="person-info"><strong>${esc(p.name)}</strong><span>${p.commits} commits${p.bot?' · bot':''}</span></span><span class="person-volume">${compact(p.added+p.removed)}</span></button>`).join('')}</div>`:'<p class="muted-text">No contributors in this period.</p>'}<div class="section-heading"><h3>Recent activity</h3><span>${rangeLabel()}</span></div>${renderTimeline(records,state.days,true)}<div class="section-heading"><h3>Open pull requests</h3><span>${loadingPRs()?'…':prCoverageUnknown(scope.repo,scope.path)?related.length?related.length+' + ?':'?':related.length}</span></div>${related.length?related.slice(0,3).map(p=>`<button class="pr-mini" data-action="pr" data-value="${esc(p.repo+'#'+p.number)}"><span class="pr-mini-number">#${p.number}</span><strong>${esc(p.title)}</strong><span><span class="${p.author.toLowerCase()===(state.snapshot.viewer||'').toLowerCase()?'own-author':''}">${esc(p.author)}</span> · ${renderPRAge(p)}</span></button>`).join(''):`<p class="muted-text">${loadingPRs()?'Loading pull requests from GitHub…':state.snapshot.mode==='cache'?'PR context is unavailable offline.':prCoverageUnknown(scope.repo,scope.path)?'PR evidence is incomplete or stale for this scope.':'No matching PRs in this snapshot.'}</p>`}${scope.repo?`<button class="button wide subtle" data-action="context-prs" data-value="${esc(scope.repo)}">View repository PRs</button><button class="button wide subtle" data-action="open-path" data-repo="${esc(scope.repo)}" data-value="${esc(scope.path||'')}">Open ${scope.path?'path':'repository'} on GitHub ${icon('link')}</button>`:''}`;
}
function renderCommits(records,showFiles=false){
 const sorted=uniqueCommits(records).sort((a,b)=>dateValue(b.date)-dateValue(a.date));
 return sorted.length?`<div class="commit-list">${sorted.slice(0,showFiles?commitLimit:100).map(c=>`<article class="commit"><span class="commit-node"></span><div><button class="commit-title" data-action="open-commit" data-repo="${esc(c.repo)}" data-value="${esc(c.id)}">${esc(c.subject||'Untitled commit')}</button><p>${esc(c.author)} <span>· ${age(c.date)} · <code>${esc(c.id.slice(0,7))}</code></span>${c.ai?' <span class="badge muted">AI assisted</span>':''}</p>${showFiles?`<div class="commit-files">${(c.files||[]).map(f=>`<span>${esc(f.path)}</span>`).join('')}</div>`:''}</div>${delta(c.added,c.removed,c.linesUnknown)}</article>`).join('')}${showFiles&&sorted.length>commitLimit?`<button class="button wide subtle" data-action="more-commits">Show 100 more commits (${number(sorted.length-commitLimit)} remaining)</button>`:''}</div>`:empty('No commits in this view','Expand the date range to explore more history.');
}
function renderTimeline(records,days=30,small=false){
 if(state.since)days=effectiveDays();
 const series=timelineSeries(records,days,referenceTime());const max=maximum(series,p=>p.commits);
 return `<div class="timeline ${small?'small':''}" role="img" aria-label="Commit activity over ${rangeLabel()}">${series.map(p=>`<div class="timeline-column" title="${esc(new Date(p.date).toLocaleDateString())}: ${p.commits} commits, +${p.added} −${p.removed}"><span style="height:${p.commits?Math.max(5,p.commits/max*100):2}%" class="${p.commits?'':'quiet'}"></span></div>`).join('')}</div><div class="chart-axis"><span>${new Date(series[0].date).toLocaleDateString(undefined,{month:'short',day:'numeric'})}</span><span>${days&&days<=30?'Daily':days&&days<=180?'Weekly':'Monthly'} · ${new Date(series.at(-1).date).toLocaleDateString(undefined,{month:'short',day:'numeric'})}</span></div>`;
}
function renderPeople(total){
 const s=state.people;const records=currentCommits();const list=sortRows(people(records).filter(p=>`${p.name} ${p.email}`.toLowerCase().includes(s.query.toLowerCase())),s.sort,s.reverse);const selected=list.find(p=>p.id===s.selected)||list[0];if(selected)s.selected=selected.id;
 const personMaximum=maximum(list,p=>p.added+p.removed);
 const selectedRecords=selected?selected.records:[];const byRepo=selected?activityRows(selectedRecords,visibleRepositories(),{}).filter(r=>r.commits).sort((a,b)=>(b.added+b.removed)-(a.added+a.removed)):[];
 const repoMaximum=maximum(byRepo,r=>r.added+r.removed);
 const days=effectiveDays()||365;const heat=dailySeries(selectedRecords,days<=180?days:365,referenceTime());const max=maximum(heat,p=>p.commits);
 return `<section class="board-summary people-summary"><div class="summary-main"><span class="metric-label">CONTRIBUTORS</span><strong>${list.length}</strong></div><div><span class="metric-label">COMMITS</span><strong>${number(total.commits)}</strong></div><div><span class="metric-label">AI ASSISTED COMMITS</span><strong>${total.commits?Math.floor(total.ai/total.commits*100):0}<small>%</small></strong></div><p class="summary-description">Explore contribution patterns<br>across repositories and time.</p></section>${filterToolbar('people')}<div class="board-split people-split"><section class="board-panel"><div class="panel-heading"><h2>Contributors</h2><span>${list.length} in scope</span></div><div class="contributor-list">${list.length?list.map((p,i)=>`<button class="contributor-row ${selected?.id===p.id?'selected':''}" data-action="select-person" data-value="${esc(p.id)}"><span class="avatar color-${i%5}">${esc(initials(p.name))}</span><span class="contributor-name"><strong>${esc(p.name)}${p.bot?'<span class="badge muted">BOT</span>':''}</strong><span>${p.commits} commits · ${p.repos.length} repositories</span>${bar(p.added,p.removed,personMaximum)}</span><span class="contributor-change">${compact(p.added+p.removed)}<small>lines</small></span></button>`).join(''):empty('No contributors match','Try another name or include bot contributions.')}</div></section><section class="board-panel contributor-detail">${selected?`<div class="detail-person"><span class="avatar large">${esc(initials(selected.name))}</span><div><div class="eyebrow">CONTRIBUTOR</div><h2>${esc(selected.name)}</h2><p>${esc(selected.email||'No email recorded')}</p></div></div><div class="person-stats"><div><strong>${number(selected.commits)}</strong><span>commits</span></div><div><strong>${selected.repos.length}</strong><span>repositories</span></div><div><strong>${new Set(selectedRecords.map(c=>new Date(c.date).toDateString())).size}</strong><span>active days</span></div><div>${delta(selected.added,selected.removed,selected.unknown)}<span>lines changed</span></div></div><div class="derived-metrics"><span>AI assisted <strong>${selected.commits?Math.floor(selected.ai/selected.commits*100):0}%</strong></span><span>Net lines <strong>${number(selected.added-selected.removed)}</strong></span><span>Churn <strong>${selected.added?(selected.removed/selected.added).toFixed(2):'0.00'}</strong></span></div><div class="section-heading"><h3>Contribution rhythm</h3><span>${rangeLabel()}</span></div>${renderTimeline(selectedRecords,days)}<div class="heatmap" role="img" aria-label="Daily commit heatmap">${heat.map(p=>`<span class="heat-cell level-${p.commits?Math.max(1,Math.ceil(p.commits/max*4)):0}" title="${esc(new Date(p.date).toLocaleDateString())}: ${p.commits} commits"></span>`).join('')}</div><div class="section-heading"><h3>Where the work happened</h3><span>${byRepo.length} repositories</span></div><div class="repo-breakdown">${byRepo.map(r=>`<button data-action="activity-repo" data-value="${esc(r.id)}"><span>${esc(r.label)}</span>${bar(r.added,r.removed,repoMaximum)}<strong>${compact(r.added+r.removed)}</strong></button>`).join('')}</div><div class="section-heading"><h3>Recent commits</h3><span>${selected.commits}</span></div>${renderCommits(selectedRecords.slice().sort((a,b)=>dateValue(b.date)-dateValue(a.date)).slice(0,8))}`:empty('Choose a contributor','Select a person to explore their work.')}</section></div>`;
}
function prSelect(field,label,options){const s=state.prs;return `<label><span class="sr-only">${label}</span><select data-field="${field}" data-board="prs" aria-label="${label}"><option value="">${facetValues(s[field]).length>1?facetValues(s[field]).length+' selected':label}</option>${options.map(([v,t])=>`<option value="${esc(v)}" ${facetValues(s[field]).length===1&&facetValues(s[field])[0]===v?'selected':''}>${esc(t)}</option>`).join('')}</select></label>`;}
function renderPRs(){
 const s=state.prs;const list=prList();const selected=list.find(p=>p.repo+'#'+p.number===s.selected)||list[0];if(selected)s.selected=selected.repo+'#'+selected.number;
 const authors=[...new Set(state.snapshot.pullRequests.map(p=>p.author))].sort();const attention=filterPRs(state.snapshot,sharedFilters(),referenceTime()).filter(p=>(p.reviewers||[]).includes(state.snapshot.viewer)&&state.snapshot.viewer).length;
 return `${state.prContext?`<div class="context-return"><button class="text-button" data-action="return-activity">← Back to Activity</button><span>${esc(state.prContext.repo)} · Shared viewing scope</span></div>`:''}<div class="pr-context"><div><span class="metric-label">OPEN IN THIS VIEW</span><strong>${loadingPRs()?'—':prCoverageUnknown(state.prContext?.repo)?list.length?list.length+' + ?':'?':list.length}</strong></div><div><span class="metric-label">AWAITING YOUR REVIEW</span><strong>${!prCoverageUnknown(state.prContext?.repo)&&state.snapshot.viewer?attention:'—'}</strong></div><p>${state.snapshot.viewer?`Signed in as <strong>${esc(state.snapshot.viewer)}</strong>`:'Reviewer matching requires a GitHub account.'}<br><span>Checks and reviews are reported evidence, not merge readiness.</span></p></div>${filterToolbar('prs')}${facetValues(state.prs.mine).length&&!state.snapshot.viewer?'<p class="coverage-note">Your GitHub account is unavailable. Mine filters cannot match until account lookup succeeds.</p>':''}<div class="pr-filters">${prSelect('mine','Everyone',mineOptions)}${prSelect('author','All authors',authors.map(a=>[a,a]))}${prSelect('review','Any review',[['REVIEW_REQUIRED','Needs review'],['APPROVED','Approved'],['CHANGES_REQUESTED','Changes requested'],['UNKNOWN','Unknown']])}${prSelect('checks','Any checks',[['SUCCESS','Passing'],['FAILURE','Failing'],['PENDING','Pending'],['UNKNOWN','Unknown']])}${prSelect('state','Any state',[['open','Ready for review'],['draft','Draft']])}<button class="text-button" data-action="reset-pr">Reset filters</button></div><div class="board-split pr-split"><section class="board-panel pr-list"><div class="panel-heading"><h2>Pull requests</h2><span>${loadingPRs()?'Loading…':list.length+' known results'+(prCoverageUnknown(state.prContext?.repo)?' · partial':'')}</span></div>${list.length?list.map(p=>`<button class="pr-row ${selected?.repo===p.repo&&selected?.number===p.number?'selected':''}" data-action="select-pr" data-value="${esc(p.repo+'#'+p.number)}"><span class="pr-state-icon ${p.draft?'draft':''}">${p.draft?'◇':'⑂'}</span><span class="pr-row-content"><span class="pr-row-meta">${esc(p.repo)} <strong>#${p.number}</strong>${p.draft?'<span class="badge muted">Draft</span>':''}</span><strong class="pr-title">${esc(p.title)}</strong><span class="pr-row-footer"><span><span class="${p.author.toLowerCase()===(state.snapshot.viewer||'').toLowerCase()?'own-author':''}">${esc(p.author)}</span> · ${renderPRAge(p)}</span><span>${badge(p.review)}${badge(p.checks)}</span></span></span></button>`).join(''):loadingPRs()?empty('Loading pull requests','GitHub reviews and checks will appear when the refresh completes.'):state.snapshot.mode==='cache'?empty('PR context is unavailable offline','Choose GitHub in workspace settings to load open pull requests.'):prCoverageUnknown(state.prContext?.repo)?empty('Pull request evidence unavailable','The inventory is incomplete or stale. Refresh or open Data status to inspect the failure.'):empty('No pull requests match','Adjust your filters or refresh to check for new work.')}</section><aside class="inspector pr-inspector" tabindex="0" aria-label="Selected pull request details">${selected?renderPRDetail(selected):'<p class="muted-text">Select a pull request to inspect its context.</p>'}</aside></div>`;
}
function renderPRDetail(p){
 return `<div class="inspector-eyebrow">PULL REQUEST <span>#${p.number}</span></div><h2>${esc(p.title)}</h2><p class="inspector-path">${esc(p.repo)}</p><button class="button primary wide" data-action="open-url" data-value="${esc(p.url)}" ${safeURL(p.url)?'':'disabled'}>Open on GitHub ${icon('link')}</button><dl class="pr-facts"><div><dt>Author</dt><dd>${esc(p.author)}</dd></div><div><dt>State</dt><dd>${p.draft?'Draft':'Open'}</dd></div><div><dt>Review</dt><dd>${badge(p.review)}</dd></div><div><dt>Checks</dt><dd>${badge(p.checks)}</dd></div><div><dt>Updated</dt><dd title="${esc(fullDate(p.updatedAt))}">${renderPRAge(p)}</dd></div><div><dt>Merge conflicts</dt><dd>${esc(p.mergeable==='CONFLICTING'?'Conflicts':p.mergeable==='MERGEABLE'?'No conflicts reported':'Unknown')}</dd></div><div><dt>Files</dt><dd>${p.fileCount??p.files?.length??0}${p.filesComplete===false?' · partial':''}</dd></div><div><dt>Reviewers</dt><dd>${esc((p.reviewers||[]).join(', ')||'None requested')}</dd></div></dl>${!p.complete?`<p class="coverage-note">${[p.inventoryComplete===false?'PR inventory incomplete':'',p.filesComplete===false?'File evidence incomplete':'',p.reviewersComplete===false?'Reviewer list incomplete':'',p.reviewsComplete===false?'Review history incomplete':''].filter(Boolean).join(' · ')||'Some supporting context is unavailable'}. Open GitHub for the complete record.</p>`:''}<div class="section-heading"><h3>Changed files</h3><span>${p.files?.length||0}</span></div><div class="file-list">${p.files?.length?p.files.map(f=>`<span>${icon('file')}${esc(f)}</span>`).join(''):'<p class="muted-text">No file evidence available.</p>'}</div>`;
}
function openModal(type){
 lastFocus=document.activeElement;state.modal=type;state.dialogError='';
 if(type==='settings'){state.settingsDraft=structuredClone(state.snapshot.settings);state.settingsSavedSelection={repositories:[...(state.snapshot.settings.repositories||[])],excludedRepositories:state.snapshot.settings.excludedRepositories==null?null:[...state.snapshot.settings.excludedRepositories]};state.discovered=[...state.snapshot.repositories].sort((a,b)=>dateValue(b.pushedAt)-dateValue(a.pushedAt)||a.id.localeCompare(b.id));state.settingsReview=false;state.settingsDiscovered=false;state.settingsTouched=new Map();state.settingsDraft.repositories=state.discovered.filter(r=>Array.isArray(state.settingsDraft.excludedRepositories)?!state.settingsDraft.excludedRepositories.includes(r.id):!state.settingsDraft.repositories?.length||state.settingsDraft.repositories.includes(r.id)).map(r=>r.id);}
 if(type==='repositories')state.repoDraft=[...state.repos];
 if(['dates','filters'].includes(type))state.filterDraft={days:state.days,since:state.since,repos:[...state.repos],board:structuredClone(state[state.board])};
 if(type==='identities'){const selected=people(state.snapshot.commits).find(p=>p.id===state.people.selected);state.identityDraft={primaryEmail:selected?.email||'',emails:selected?.email?[selected.email]:[],name:''};}
 if(type==='controls')state.controlsDraft=(state.controls?.repositories||[]).filter(r=>r.included).map(r=>r.id);
 renderModal();
}
function closeModal(){state.modal=null;$('#modal-root').innerHTML='';if(lastFocus?.isConnected)lastFocus.focus();}
function renderModal(){
 if(!state.modal){$('#modal-root').innerHTML='';return;}
 const titles={settings:'Workspace settings',repositories:'Repository scope',status:'Data status',help:'Keyboard guide',dates:'Date range',filters:'Filters',identities:'Contributor identities',identityReview:'Review identity changes',controls:'Repository controls',removeCache:'Remove cached repository',sort:'Sort pull requests'};
 let content='';let footer='<button class="button" data-action="close-modal">Done</button>';
 const parity=renderParityModal();
 if(parity){content=parity.content;footer=parity.footer;}else if(state.modal==='settings'){
 const draft=state.settingsDraft;
 content=`<p class="modal-intro">Choose the repositories and data source for your Bigboard workspace.</p><div class="mode-options" role="group" aria-label="Data source">${[['live','GitHub','Refresh repositories and PRs'],['cache','Cached history','Read your existing local cache']].map(([mode,title,description])=>`<button data-action="settings-mode" data-value="${mode}" class="mode-option ${draft.mode===mode?'selected':''}" aria-pressed="${draft.mode===mode}"><strong>${title}</strong><span>${description}</span></button>`).join('')}</div><div class="settings-fields"><label>Personal account<input id="personalAccount" name="personalAccount" autocomplete="off" value="${esc(draft.personalAccount)}" placeholder="GitHub username"></label><label>Organizations<input id="organizations" name="organizations" autocomplete="off" value="${esc((draft.organizations||[]).join(', '))}" placeholder="Separate names with commas"></label></div><p class="field-help">${draft.mode==='live'?'Git and GitHub CLI (gh) must be installed. To sign in, run <code>gh auth login</code>, then return to Bigboard to discover repositories. Refresh reads default-branch history and open PRs.':'Reads owned Bigboard caches without network requests. Uncached repositories will have no history.'}</p><div class="section-heading"><h3>Tracked repositories</h3><button class="button small" data-action="discover" ${state.busy?'disabled':''}>${state.busy?'Working…':'Discover repositories'}</button></div><p class="field-help">Discover the full repository list for your current sources before reviewing, then choose at least one repository. Newly discovered repositories are included unless you exclude them.</p><div class="repository-choices">${state.discovered.length?state.discovered.map(r=>`<label><input type="checkbox" data-settings-repo="${esc(r.id)}" ${(draft.repositories||[]).includes(r.id)?'checked':''}><span>${esc(r.id)}${r.pushedAt?`<small class="repo-pushed">Last push ${esc(fullDate(r.pushedAt))}</small>`:''}</span></label>`).join(''):'<p class="muted-text">Discover repositories after entering your account or organizations.</p>'}</div>`;
 if(state.settingsReview)content=`<p class="modal-intro">Review your workspace before saving.</p><dl class="status-facts"><div><dt>Data source</dt><dd>${esc(draft.mode)}</dd></div><div><dt>Account</dt><dd>${esc(draft.personalAccount||'None')}</dd></div><div><dt>Organizations</dt><dd>${esc((draft.organizations||[]).join(', ')||'None')}</dd></div><div><dt>Repositories</dt><dd>${draft.repositories.length?draft.repositories.map(esc).join('<br>'):'All discovered repositories'}</dd></div></dl><button class="text-button" data-action="edit-settings">Back to editing</button>`;
 footer=`<button class="button" data-action="close-modal">Cancel</button><button class="button primary" data-action="save-settings" ${state.busy?'disabled':''}>${state.busy?'Saving…':state.settingsReview?'Save workspace':'Review workspace'}</button>`;
 }else if(state.modal==='repositories'){
 content=`<p class="modal-intro">This viewing scope applies to all three boards. It does not change which repositories are refreshed.</p><div class="scope-actions"><button class="text-button" data-action="scope-all">All repositories</button><span>${state.repoDraft.length?state.repoDraft.length+' selected':'All included'}</span></div><div class="repository-choices">${state.snapshot.repositories.map(r=>`<label><input type="checkbox" data-scope-repo="${esc(r.id)}" ${state.repoDraft.includes(r.id)?'checked':''}><span>${esc(r.id)}</span></label>`).join('')}</div>`;
 footer='<button class="button" data-action="close-modal">Cancel</button><button class="button primary" data-action="apply-scope">Apply scope</button>';
 }else if(state.modal==='status'){
 const snap=state.snapshot;
 content=`<div class="status-overview"><span class="status-dot ${snap.status.errors?.length?'warning':''}"></span><div><h3>${snap.status.errors?.length?'Some data needs attention':'Snapshot available'}</h3><p>${esc(snap.status.message||'Your current snapshot is ready to explore.')}</p></div></div><dl class="status-facts"><div><dt>Source</dt><dd>${esc({cache:'Offline repository cache',live:'GitHub and local Git history'}[snap.mode])}</dd></div><div><dt>Snapshot</dt><dd>${esc(fullDate(snap.generatedAt))}</dd></div><div><dt>Coverage</dt><dd>${snap.repositories.length} repositories · ${number(snap.commits.length)} commit records · ${snap.pullRequests.length} PRs</dd></div><div><dt>Last look</dt><dd>${esc(bookmark()?fullDate(bookmark()):state.snapshot.bookmark?.message||'No prior fetched visit')}</dd></div><div><dt>Next bookmark</dt><dd>${esc(state.snapshot.bookmark?.eligibleAt?fullDate(state.snapshot.bookmark.eligibleAt):'No eligible fetched snapshot')}</dd></div><div><dt>GitHub requests</dt><dd>${snap.status.probeRequests||0} probes · ${snap.status.inventoryRequests||0} inventories · ${snap.status.fileRequests||0} file pages · ${snap.status.accountRequests||0} account lookups</dd></div><div><dt>Cache size</dt><dd>${state.controls?number(Math.round(state.controls.cacheBytes/1048576))+' MB':'Open Repository controls for storage details'}</dd></div><div><dt>Cache location</dt><dd>${esc(snap.status.cachePath||'Not available')}</dd></div></dl>${snap.status.errors?.length?`<h3 class="status-heading">Diagnostics</h3><ul class="diagnostics">${snap.status.errors.map(e=>`<li>${esc(e)}</li>`).join('')}</ul>`:''}<h3 class="status-heading">PR evidence coverage</h3><p class="field-help">${snap.pullRequests.filter(p=>p.inventoryComplete===false).length} records with incomplete inventory · ${snap.pullRequests.filter(p=>p.filesComplete===false).length} incomplete file lists · ${snap.pullRequests.filter(p=>p.reviewersComplete===false||p.reviewsComplete===false).length} incomplete reviewer histories. Missing evidence remains unknown.</p><h3 class="status-heading">Repository coverage</h3><div class="coverage-list">${snap.repositories.map(r=>`<div><span>${esc(r.id)}<small>${esc(r.branch||'Default branch')}${r.revision?' · '+esc(r.revision.slice(0,7)):''}</small></span><span class="badge ${r.stale?'pending':'muted'}">${r.stale?'Stale':'Available'}</span>${r.error?`<p>${esc(r.error)}</p>`:''}</div>`).join('')}</div><p class="field-help">The date and repository filters only change your view. Refresh runs when you request it; background polling is not enabled.</p>`;
 }else{
 content=`<p class="modal-intro">Move between boards without losing your filters or position. Shortcuts stay out of the way while you type.</p><div class="shortcut-list">${[['a','Activity board'],['c','Contributors board'],['p','Pull requests board'],['/','Search the current board'],['r','Refresh workspace'],['f / t','Filters / date range'],['s / S','Sort / reverse'],['x','Reset this board'],['e / i','Data status / repository controls'],['1–4','Activity evidence lenses'],['v','Activity repository PRs'],['m / M / n','PR attention / authored / requested'],['A / z','PR author sort / sort picker'],['y','Copy this view as Markdown'],[',','Workspace settings'],['?','Keyboard guide'],['Esc','Close dialog or go up one directory'],['Enter','Explore a focused activity row'],['↑ ↓','Move between activity rows'],['Tab','Move between controls']].map(([key,description])=>`<div><kbd>${key}</kbd><span>${description}</span></div>`).join('')}</div><p class="field-help">Click a repository or directory to inspect its contributors. Use the arrow at the end of the row, double-click, or press Enter to explore it.</p>`;
 }
 const old=$('#modal-root dialog');const focusID=document.activeElement?.id;
 $('#modal-root').innerHTML=`<dialog class="modal ${state.modal==='settings'?'settings-modal':''}" aria-labelledby="modal-title"><header><div class="eyebrow">BIGBOARD</div><h2 id="modal-title">${titles[state.modal]}</h2><button class="icon-button modal-close" data-action="close-modal" aria-label="Close dialog">×</button></header><div class="modal-body">${state.dialogError?`<div class="dialog-error" role="alert">${esc(state.dialogError)}</div>`:''}${content}</div><footer>${footer}</footer></dialog>`;
 const dialog=$('#modal-root dialog');dialog.showModal();dialog.addEventListener('cancel',e=>{e.preventDefault();closeModal();});
 if(old&&focusID)document.getElementById(focusID)?.focus();
}
function settingsSources(){return JSON.stringify([state.settingsDraft.mode,state.settingsDraft.personalAccount,state.settingsDraft.organizations]);}
function readSettings(){
 const previous=settingsSources();
 if($('#personalAccount'))state.settingsDraft.personalAccount=$('#personalAccount').value.trim();
 if($('#organizations'))state.settingsDraft.organizations=$('#organizations').value.split(',').map(s=>s.trim()).filter(Boolean);
 if(settingsSources()!==previous)state.settingsDiscovered=false;
 return state.settingsDraft;
}
async function runWithSnapshots(method,arg){
 const version=++refreshVersion;let pending=true;let timer;
 if(method==='SaveSettings')prEvidenceLoaded=false;
 const fingerprint=snapshot=>[snapshot.generatedAt,snapshot.status.phase,snapshot.status.message,snapshot.commits.length,snapshot.repositories.length,snapshot.pullRequests.length].join('|');
 let last=fingerprint(state.snapshot);
 const poll=async()=>{
  if(!pending||version!==refreshVersion)return;
  try{
   const next=validSnapshot(await api('Snapshot'));const signature=fingerprint(next);
   if(pending&&version===refreshVersion&&signature!==last){state.snapshot=next;last=signature;render();}
  }catch{}
  if(pending&&version===refreshVersion)timer=setTimeout(poll,1000);
 };
 timer=setTimeout(poll,500);
 try{const result=validSnapshot(await api(method,arg));prEvidenceLoaded=result.mode==='live'&&(result.status.phase==='ready'||result.pullRequests.length>0);return result;}finally{pending=false;clearTimeout(timer);}
}
async function refresh(){
 if(state.busy)return;state.busy=true;state.error='';render();
 try{state.snapshot=await runWithSnapshots('Refresh');if(state.snapshot.status.phase!=='setup')toast('Workspace refreshed');}catch(e){state.error=e.message;}finally{state.busy=false;render();if(state.snapshot.status.phase==='setup'&&!state.modal)openModal('settings');}
}

async function copyView(){
 const cell=value=>String(value??'').replaceAll('|','\\|').replaceAll('\n',' ');
 let heading,columns,rows;
 if(state.board==='prs'){
  heading='Pull requests';columns=['Repository','PR','Title','Author','Review','Checks'];rows=prList().map(p=>[p.repo,'#'+p.number,p.title,p.author,statusName(p.review),statusName(p.checks)]);
 }else if(state.board==='people'){
  heading='Contributors';columns=['Contributor','Commits','Added','Removed','Repositories'];rows=sortRows(people(currentCommits()).filter(p=>`${p.name} ${p.email}`.toLowerCase().includes(state.people.query.toLowerCase())),state.people.sort,state.people.reverse).map(p=>[p.name,p.commits,p.added,p.removed,p.repos.length]);
 }else{
  heading=scopeTitle();
  if(state.activity.scope.file){columns=['Commit','Subject','Author','Added','Removed'];rows=uniqueCommits(scopedCommits(currentCommits(),state.activity.scope)).filter(c=>`${c.subject} ${c.author}`.toLowerCase().includes(state.activity.query.toLowerCase())).map(c=>[c.id.slice(0,7),c.subject,c.author,c.added,c.removed]);}
  else{columns=['Name','Commits','Added','Removed','Contributors'];rows=sortRows(activityRows(currentCommits(),visibleRepositories(),state.activity.scope),state.activity.sort,state.activity.reverse).filter(r=>`${r.label} ${r.id}`.toLowerCase().includes(state.activity.query.toLowerCase())).map(r=>[r.id,r.commits,r.added,r.removed,r.people]);}
 }
 const text=`## ${heading}\n\n${rangeLabel()}\n\n| ${columns.join(' | ')} |\n| ${columns.map(()=>'---').join(' | ')} |\n${rows.map(row=>'| '+row.map(cell).join(' | ')+' |').join('\n')}`;
 if(window.go?.main?.App?.CopyText)await window.go.main.App.CopyText(text);else await navigator.clipboard.writeText(text);
 toast('Copied this view as Markdown');
}

async function openURL(value){
 const url=safeURL(value);if(!url){toast('This GitHub link is unavailable.');return;}
 try{if(window.go?.main?.App?.OpenURL)await window.go.main.App.OpenURL(url);else window.open(url,'_blank','noopener,noreferrer');}catch(e){toast(e.message);}
}
function explore(id,kind){
 const s=state.activity;commitLimit=100;
 if(kind==='repository')s.scope={repo:id,path:'',file:false};else s.scope={...s.scope,path:id,file:kind==='file'};
 s.lens='ranking';s.lensPerson='';s.lensEvidence=null;s.selected='';render();
}
function back(){if(state.prContext){returnActivity();return;}const s=state.activity;if(s.lens!=='ranking'){s.lens='ranking';s.lensPerson='';s.lensEvidence=null;render();return;}if(!s.scope.repo)return;const path=s.scope.path.split('/').filter(Boolean);if(path.length){path.pop();s.scope={...s.scope,path:path.join('/'),file:false};}else s.scope={repo:'',path:'',file:false};s.lens='ranking';s.lensPerson='';s.lensEvidence=null;s.selected='';render();}
async function action(button){
 const value=button.dataset.value;
 if(await parityAction(button))return;
 switch(button.dataset.action){
 case 'board':switchBoard(value);break;
 case 'more-commits':commitLimit+=100;render();break;
 case 'range':state.days=Number(value);state.since='';render();break;
 case 'refresh':await refresh();break;
 case 'copy-view':await copyView();break;
 case 'dismiss-error':state.error='';render();break;
 case 'dates':case 'filters':case 'sort':case 'settings':case 'status':case 'help':case 'repositories':openModal(button.dataset.action);break;
 case 'close-modal':closeModal();break;
 case 'reverse':state[value].reverse=!state[value].reverse;render();break;
 case 'activity-root':state.activity.scope={repo:'',path:'',file:false};state.activity.selected='';render();break;
 case 'activity-path':state.activity.scope={...state.activity.scope,path:value,file:state.activity.scope.file&&state.activity.scope.path===value};state.activity.selected='';render();break;
 case 'explore':explore(value,button.dataset.kind);break;
 case 'activity-repo':state.board='activity';explore(value,'repository');break;
 case 'person':state.board='people';state.people.query='';state.people.selected=value;render();break;
 case 'select-person':state.people.selected=value;render();break;
 case 'pr':openContextPRs(value.split('#')[0],value);break;
 case 'select-pr':state.prs.selected=value;render();break;
 case 'open-url':await openURL(value);break;
 case 'open-repo':await openURL(`https://github.com/${value}`);break;
 case 'open-commit':await openURL(`https://github.com/${button.dataset.repo}/commit/${value}`);break;
 case 'reset-pr':state.prs={...state.prs,query:'',author:'',review:'',checks:'',state:'',mine:''};render();break;
 case 'settings-mode':readSettings();if(state.settingsDraft.mode!==value)state.settingsDiscovered=false;state.settingsDraft.mode=value;renderModal();break;
 case 'scope-all':state.repoDraft=[];renderModal();break;
 case 'apply-scope':state.repos=[...state.repoDraft];if(state.repos.length&&state.activity.scope.repo&&!state.repos.includes(state.activity.scope.repo))state.activity.scope={repo:'',path:'',file:false};closeModal();render();break;
 case 'discover':{
  readSettings();state.settingsDiscovered=false;const sources=settingsSources();const request=structuredClone({...state.settingsDraft,...state.settingsSavedSelection});state.busy=true;state.dialogError='';renderModal();
  try{const discovered=(await api('Discover',request))||[];if(settingsSources()!==sources){state.dialogError='Sources changed during discovery. Discover repositories again before reviewing.';}else{state.discovered=discovered.sort((a,b)=>dateValue(b.pushedAt)-dateValue(a.pushedAt)||a.id.localeCompare(b.id));state.settingsDiscovered=true;state.settingsDraft.repositories=state.discovered.filter(r=>state.settingsTouched.has(r.id)?state.settingsTouched.get(r.id):r.included!==false).map(r=>r.id);if(!state.discovered.length)state.dialogError='No repositories were found for these sources.';}}catch(e){state.dialogError=e.message;}finally{state.busy=false;renderModal();}break;
 }
 case 'save-settings':{
  readSettings();if(!state.settingsReview){if(!state.settingsDiscovered){state.dialogError='Discover repositories for the current sources before reviewing.';renderModal();break;}if(!state.discovered.length||!state.settingsDraft.repositories.length){state.dialogError='Discover repositories and select at least one before reviewing.';renderModal();break;}const known=new Set(state.discovered.map(r=>r.id));state.settingsDraft.excludedRepositories=[...(state.settingsDraft.excludedRepositories||[]).filter(id=>!known.has(id)),...state.discovered.filter(r=>!state.settingsDraft.repositories.includes(r.id)).map(r=>r.id)];state.dialogError='';state.settingsReview=true;renderModal();break;}state.busy=true;state.dialogError='';renderModal();
  try{state.snapshot=await runWithSnapshots('SaveSettings',state.settingsDraft);state.repos=[];state.activity.scope={repo:'',path:'',file:false};state.activity.selected='';state.people.selected='';state.prs.selected='';closeModal();toast('Workspace saved');}catch(e){state.dialogError=e.message;}finally{state.busy=false;render();if(state.modal)renderModal();}break;
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
 if(state.modal==='settings'&&['personalAccount','organizations'].includes(input.id))readSettings();
 if(input.dataset.field==='query'){
  const start=input.selectionStart;state[input.dataset.board].query=input.value;render();const next=document.querySelector(`[data-board="${input.dataset.board}"][data-field="query"]`);next.focus();next.setSelectionRange(start,start);
 }
});
document.addEventListener('change',event=>{
 const input=event.target;
 if(parityChange(input))return;
 if(input.dataset.field){state[input.dataset.board][input.dataset.field]=input.type==='checkbox'?input.checked:input.value;render();}
 if(input.dataset.settingsRepo){state.settingsTouched.set(input.dataset.settingsRepo,input.checked);const repos=new Set(state.settingsDraft.repositories||[]);input.checked?repos.add(input.dataset.settingsRepo):repos.delete(input.dataset.settingsRepo);state.settingsDraft.repositories=[...repos];}
 if(input.dataset.scopeRepo){const repos=new Set(state.repoDraft);input.checked?repos.add(input.dataset.scopeRepo):repos.delete(input.dataset.scopeRepo);state.repoDraft=[...repos];}
});
document.addEventListener('keydown',event=>{
 if(parityKey(event))return;
 if(state.modal)return;
 if(event.ctrlKey||event.metaKey||event.altKey||['INPUT','TEXTAREA','SELECT'].includes(event.target.tagName)||event.target.isContentEditable)return;
 const boards={a:'activity',c:'people',p:'prs'};
 if(boards[event.key]){switchBoard(boards[event.key]);event.preventDefault();}
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
function applyPreferences(){
 if(preferencesApplied)return;preferencesApplied=true;
 const since=state.snapshot.preferences?.since||'14d';state.days=since==='all'?0:since==='1y'?365:Number.parseInt(since,10)||14;
 const sort=state.snapshot.preferences?.sort||'total';state.people.sort=['total','impact'].includes(sort)?'change':sort;
}
function switchBoard(board){
 if(state.prContext){state.prs=state.prContext.saved;state.prContext=null;}
 closeModal();state.board=board;render();
}
function openContextPRs(repo,selected=''){
 if(!state.prContext)state.prContext={repo,saved:structuredClone(state.prs)};else state.prContext.repo=repo;
 state.prs={...state.prs,query:'',author:'',review:'',checks:'',state:'',mine:'',selected};state.board='prs';render();
}
function returnActivity(){if(state.prContext){state.prs=state.prContext.saved;state.prContext=null;}state.board='activity';render();}
function resetBoard(target){return {...target,query:'',people:[],hideBots:false,author:'',review:'',checks:'',state:'',mine:''};}
function renderPRAge(pr){
 const checked=dateValue(pr.checkedAt)||dateValue(state.snapshot.generatedAt)||Date.now();const inactive=dateValue(pr.updatedAt)>0&&checked-dateValue(pr.updatedAt)>=14*86400000;
 return `${age(pr.updatedAt)}${inactive?'<span class="inactivity-marker" title="No update for at least 14 days">!</span>':''}${afterBookmark(pr.updatedAt)?'<span class="bookmark-marker" title="Updated since last look">+</span>':''}`;
}
function selectedActivityScope(){
 const s=state.activity;if(s.scope.file)return s.scope;
 return s.selected?(s.scope.repo?{repo:s.scope.repo,path:s.selected,file:false}:{repo:s.selected,path:'',file:false}):s.scope;
}
function renderLensTabs(){
 const labels=[['ranking','Directories','4'],['commits','Commits','1'],['people','People','2'],['related','Related','3']];
 return `<div class="lens-tabs" role="group" aria-label="Activity evidence">${labels.map(([value,label,key])=>`<button data-action="lens" data-value="${value}" aria-pressed="${state.activity.lens===value}">${label}<kbd>${key}</kbd></button>`).join('')}</div>`;
}
function renderLens(records){
 const s=state.activity;
 if(s.lensEvidence){const allowed=new Set(s.lensEvidence.values);records=s.lensEvidence.mode==='people'?records.filter(c=>allowed.has(identity(c))):records.filter(c=>allowed.has(c.id));}
 if(s.lens==='commits')return renderCommits(records,true);
 if(s.lens==='people'){
  const list=people(records);
  if(s.lensPerson){const selected=list.find(p=>p.id===s.lensPerson);return `<button class="text-button lens-back" data-action="lens-person" data-value="">← All contributors in this scope</button>${selected?renderCommits(selected.records,true):empty('No matching contributor','Change your scope or return to the contributor list.')}`;}
  return `<div class="scope-people">${list.length?list.map(p=>`<button class="person-mini" data-action="lens-person" data-value="${esc(p.id)}"><span class="avatar">${esc(initials(p.name))}</span><span class="person-info"><strong>${esc(p.name)}</strong><span>${p.commits} scoped commits</span></span>${delta(p.added,p.removed,p.unknown)}${icon('chevron')}</button>`).join(''):empty('No contributors in this scope','Expand the date or repository selection.')}</div>`;
 }
 const related=s.scope.repo?relatedScopes(currentCommits(),s.scope,s.relatedMode,state.snapshot.workAreas||{}):[];
 return `<div class="related-evidence"><div class="related-toggle"><button class="button small" data-action="related-mode" data-value="commits" aria-pressed="${s.relatedMode==='commits'}">Shared commits</button><button class="button small" data-action="related-mode" data-value="people" aria-pressed="${s.relatedMode==='people'}">Contributor overlap</button></div><p class="field-help">${s.relatedMode==='people'?'Historical contributor overlap indicates shared context, not ownership or online presence.':'Scopes connected by exact commit identities in the selected period.'}</p>${related.length?related.map(r=>`<button class="related-row" data-action="related-open" data-area="${esc(r.areaName||'')}" data-repo="${esc(r.repo)}" data-value="${esc(r.path)}"><span><strong>${esc(r.repo)}</strong>${r.areaName?`<small>Named area · ${esc(r.areaName)}</small>`:r.path?`<small>${esc(r.path)}</small>`:''}</span><span>${r.count} shared ${s.relatedMode==='people'?'contributors':'commits'} ›</span></button>`).join(''):empty('No related evidence','Select a repository or directory and expand the date range to find historical overlap.')}</div>`;
}
function renderRowPRs(row){
 if(state.snapshot.mode==='cache')return '';
 const scope=state.activity.scope.repo?{repo:state.activity.scope.repo,path:row.path}:{repo:row.id,path:''};
 const inventory=filterPRs(state.snapshot,sharedFilters(),referenceTime()).filter(p=>p.repo===scope.repo);
 const list=inventory.filter(p=>!scope.path||(p.files||[]).some(f=>f===scope.path||f.startsWith(scope.path+'/')));
 const repo=state.snapshot.repositories.find(r=>r.id===scope.repo);
 const partial=loadingPRs()||repo?.prStale||repo?.prInventoryComplete===false||(scope.path&&repo?.prFilesComplete===false)||inventory.some(p=>p.inventoryComplete===false)||(scope.path&&inventory.some(p=>p.filesComplete===false));
 const failures=list.filter(p=>['FAILURE','ERROR'].includes(p.checks)).length;const changes=list.filter(p=>p.review==='CHANGES_REQUESTED').length;
 return `<span class="activity-pr-counts" title="Open PRs in shared viewing scope${partial?' · incomplete evidence':''}">${partial?'?':list.length} PR${list.length===1?'':'s'}${failures?` · ${failures} failing`:''}${changes?` · ${changes} changes requested`:''}</span>`;
}
function renderDateDraft(){
 const draft=state.filterDraft;const isCustom=!draft.since&&!ranges.some(r=>r.value===draft.days);
 return `<fieldset class="filter-section"><legend>Date range</legend><div class="range-options">${ranges.map(r=>`<label><input type="radio" name="rangeChoice" value="${r.value}" ${!draft.since&&draft.days===r.value?'checked':''}>${r.label}</label>`).join('')}<label><input type="radio" name="rangeChoice" value="custom" ${isCustom?'checked':''}>Custom days</label><input id="customDays" aria-label="Custom days" type="number" min="1" max="3650" value="${isCustom?draft.days:14}"><label><input type="radio" name="rangeChoice" value="bookmark" ${draft.since?'checked':''} ${bookmark()?'':'disabled'}>Since last look</label></div><p class="field-help">${bookmark()?`Last look: ${esc(fullDate(bookmark()))}`:'No previous fetched visit is saved yet.'}</p></fieldset>`;
}
function facetSection(field,title,options,values){
 const selected=facetValues(values);return `<fieldset class="filter-section"><legend>${title}</legend><div class="facet-options">${options.map(([value,label])=>`<label><input type="checkbox" data-facet="${field}" value="${esc(value)}" ${selected.includes(value)?'checked':''}><span>${esc(label)}</span></label>`).join('')}</div></fieldset>`;
}
function renderParityModal(){
 const mode=state.modal;
 if(['filters','dates'].includes(mode)){
  const draft=state.filterDraft;
  let content=renderDateDraft();
  if(mode==='filters'){
   content+=facetSection('repos','Repository viewing scope',state.snapshot.repositories.map(r=>[r.id,r.id]),draft.repos);
   if(state.board==='activity')content+=facetSection('people','Git contributors',people(selectCommits(state.snapshot,sharedFilters(),referenceTime())).map(p=>[p.id,p.name]),draft.board.people);
   if(state.board!=='prs')content+=`<label class="checkbox-label"><input type="checkbox" data-draft-bots ${draft.board.hideBots?'checked':''}> Hide bot contributors on this board</label>`;
   else{
    content+=facetSection('mine','Mine',mineOptions,draft.board.mine);
    content+=facetSection('author','GitHub authors',[...new Set(state.snapshot.pullRequests.map(p=>p.author))].sort().map(a=>[a,a]),draft.board.author);
    content+=facetSection('review','Review',[['REVIEW_REQUIRED','Needs review'],['APPROVED','Approved'],['CHANGES_REQUESTED','Changes requested'],['UNKNOWN','Unknown']],draft.board.review);
    content+=facetSection('checks','Checks',[['SUCCESS','Passing'],['FAILURE','Failing'],['ERROR','Error'],['PENDING','Pending'],['UNKNOWN','Unknown']],draft.board.checks);
    content+=facetSection('state','State',[['open','Open'],['draft','Draft']],draft.board.state);
   }
  }
  return {content,footer:`<button class="text-button" data-action="draft-reset-board">Reset this board</button><button class="text-button" data-action="draft-reset-shared">Reset shared</button><button class="button" data-action="close-modal">Cancel</button><button class="button primary" data-action="apply-filters">Apply filters</button>`};
 }
 if(mode==='sort')return {content:`<div class="sort-options">${sortOptions('prs').map(([value,label],i)=>`<button class="button" data-action="choose-sort" data-value="${value}"><kbd>${i+1}</kbd>${label}</button>`).join('')}</div>`,footer:'<button class="button" data-action="close-modal">Cancel</button>'};
 if(mode==='identities'){
  const draft=state.identityDraft;const choices=state.identities?.identities||[];
  return {content:`<p class="modal-intro">Set a display name or combine email identities. Names alone never merge people. Changes rescan cached history without fetching.</p><label class="search-field identity-search"><input aria-label="Search identities" id="identitySearch" placeholder="Find an email or name"></label><div class="repository-choices identity-choices">${choices.map(i=>`<label data-identity-row><input type="checkbox" data-identity-email="${esc(i.email)}" ${draft.emails.includes(i.email)?'checked':''}><span>${esc(i.name)}<small>${esc(i.email)}${i.aliases?.length?' · '+esc(i.aliases.join(', ')):''}</small></span></label>`).join('')}</div><div class="settings-fields"><label>Primary email<select id="identityPrimary"><option value="">Choose an identity</option>${choices.map(i=>`<option value="${esc(i.email)}" ${draft.primaryEmail===i.email?'selected':''}>${esc(i.email)}</option>`).join('')}</select></label><label>Display name<input id="identityName" value="${esc(draft.name)}" placeholder="Blank uses automatic naming"></label></div>`,footer:'<button class="button" data-action="close-modal">Cancel</button><button class="button primary" data-action="review-identity">Review changes</button>'};
 }
 if(mode==='identityReview')return {content:`<p class="modal-intro">Save these identity changes?</p><dl class="status-facts"><div><dt>Primary email</dt><dd>${esc(state.identityDraft.primaryEmail)}</dd></div><div><dt>Emails</dt><dd>${state.identityDraft.emails.map(esc).join('<br>')}</dd></div><div><dt>Display name</dt><dd>${esc(state.identityDraft.name||'Automatic naming')}</dd></div></dl>`,footer:`<button class="button" data-action="edit-identity">Back</button><button class="button primary" data-action="save-identity" ${state.busy?'disabled':''}>Save identity</button>`};
 if(mode==='controls'){
  const controls=state.controls||{repositories:[]};
  return {content:`<p class="modal-intro">Choose repositories to track for this session. This does not change your saved settings. Only caches no longer listed by discovery can be removed.</p><div class="repository-choices">${controls.repositories.map(r=>`<div class="control-row"><label><input type="checkbox" data-control-repo="${esc(r.id)}" ${state.controlsDraft.includes(r.id)?'checked':''} ${!r.selectable?'disabled':''}><span>${esc(r.id)}<small>${r.listed?'Tracked source':r.selectable?'Available cached source':'No longer listed'}${r.cached?' · cached':''}</small></span></label>${r.canRemove?`<button class="text-button" data-action="confirm-remove-cache" data-value="${esc(r.id)}" ${state.busy?'disabled':''}>Remove cache</button>`:''}</div>`).join('')}</div><p class="field-help">Cache storage: ${number(Math.round((controls.cacheBytes||0)/1048576))} MB · ${esc(controls.cachePath||'')}</p>${controls.errors?.length?`<ul class="diagnostics">${controls.errors.map(e=>`<li>${esc(e)}</li>`).join('')}</ul>`:''}`,footer:`<button class="button" data-action="close-modal">Cancel</button><button class="button primary" data-action="apply-controls" ${state.busy?'disabled':''}>Apply tracking</button>`};
 }
 if(mode==='removeCache')return {content:`<p class="modal-intro">Remove Bigboard’s owned cache for <strong>${esc(state.removeCache)}</strong>? This deletes its locally cached history. Developer checkouts are unaffected.</p>`,footer:`<button class="button" data-action="cancel-remove-cache">Cancel</button><button class="button danger" data-action="remove-cache" ${state.busy?'disabled':''}>Remove cache</button>`};
 return null;
}
function readDateDraft(){
 const selected=document.querySelector('input[name="rangeChoice"]:checked')?.value;
 if(selected==='bookmark'){state.filterDraft.since=bookmark();state.filterDraft.days=0;}
 else{const days=selected==='custom'?Number($('#customDays').value):Number(selected);if(!Number.isInteger(days)||days<0||days>3650||(selected==='custom'&&days<1))throw new Error('Custom days must be a whole number from 1 to 3650.');state.filterDraft.days=days;state.filterDraft.since='';}
}
async function performSnapshot(method,arg){
 if(state.busy)throw new Error('Wait for the current refresh to finish.');state.busy=true;render();if(state.modal)renderModal();
 try{state.snapshot=await runWithSnapshots(method,arg);return state.snapshot;}finally{state.busy=false;render();if(state.modal)renderModal();}
}
async function parityAction(button){
 const value=button.dataset.value;const action=button.dataset.action;
 switch(action){
 case 'export-json':{if(window.go?.main?.App?.SaveExportJSON){const saved=await window.go.main.App.SaveExportJSON();if(saved)toast('Contributor statistics exported');}else{const json=await api('ExportJSON');const url=URL.createObjectURL(new Blob([typeof json==='string'?json:JSON.stringify(json,null,2)],{type:'application/json'}));const link=document.createElement('a');link.href=url;link.download='bigboard-contributors.json';link.click();setTimeout(()=>URL.revokeObjectURL(url),1000);toast('Contributor statistics exported');}break;}
 case 'status':try{state.controls=await api('RepositoryControls');}catch{}openModal('status');break;
 case 'edit-settings':state.settingsReview=false;renderModal();break;
 case 'lens':state.activity.lens=value;state.activity.lensEvidence=null;if(value==='ranking'){delete state.activity.scope.areaName;delete state.activity.scope.areaRules;}state.activity.lensPerson='';commitLimit=100;render();break;
 case 'lens-person':state.activity.lensPerson=value;commitLimit=100;render();break;
 case 'related-mode':state.activity.relatedMode=value;render();break;
 case 'related-open':{const related=relatedScopes(currentCommits(),state.activity.scope,state.activity.relatedMode,state.snapshot.workAreas||{}).find(r=>r.repo===button.dataset.repo&&r.path===value&&(r.areaName||'')===(button.dataset.area||''));if(!related)break;state.activity.scope={repo:related.repo,path:related.path,file:false,areaName:related.areaName,areaRules:state.snapshot.workAreas?.[related.repo]||[]};state.activity.lens=state.activity.relatedMode==='people'?'people':'commits';state.activity.lensPerson='';state.activity.lensEvidence={mode:state.activity.relatedMode,values:related.evidence};render();break;}
 case 'context-prs':openContextPRs(value);break;
 case 'return-activity':returnActivity();break;
 case 'open-path':{const repo=state.snapshot.repositories.find(r=>r.id===button.dataset.repo);await openURL(await api('OpenPath',{repo:button.dataset.repo,path:value,revision:repo?.revision||''}));break;}
 case 'choose-sort':state.prs.sort=value;state.prs.reverse=false;closeModal();render();break;
 case 'draft-reset-board':readDateDraft();state.filterDraft.board=resetBoard(state.filterDraft.board);renderModal();break;
 case 'draft-reset-shared':state.filterDraft.days=0;state.filterDraft.since='';state.filterDraft.repos=[];renderModal();break;
 case 'apply-filters':try{readDateDraft();state.days=state.filterDraft.days;state.since=state.filterDraft.since;state.repos=state.filterDraft.repos;state[state.board]=state.filterDraft.board;closeModal();render();}catch(e){state.dialogError=e.message;renderModal();}break;
 case 'identities':state.identities=await api('IdentityOptions');openModal('identities');break;
 case 'review-identity':{
  state.identityDraft.primaryEmail=$('#identityPrimary').value;state.identityDraft.name=$('#identityName').value.trim();if(!state.identityDraft.primaryEmail){state.dialogError='Choose a primary email.';renderModal();break;}if(!state.identityDraft.emails.includes(state.identityDraft.primaryEmail))state.identityDraft.emails.push(state.identityDraft.primaryEmail);state.modal='identityReview';state.dialogError='';renderModal();break;
 }
 case 'edit-identity':state.modal='identities';renderModal();break;
 case 'save-identity':try{await performSnapshot('SaveIdentity',state.identityDraft);closeModal();toast('Identity saved');}catch(e){state.dialogError=e.message;renderModal();}break;
 case 'controls':state.controls=await api('RepositoryControls');openModal('controls');break;
 case 'apply-controls':try{await performSnapshot('ApplyRepositoryControls',{included:state.controlsDraft});closeModal();}catch(e){state.dialogError=e.message;renderModal();}break;
 case 'confirm-remove-cache':state.removeCache=value;state.modal='removeCache';renderModal();break;
 case 'cancel-remove-cache':state.modal='controls';renderModal();break;
 case 'remove-cache':try{state.controls=await api('RemoveCachedRepository',state.removeCache);state.modal='controls';renderModal();toast('Cache removed');}catch(e){state.dialogError=e.message;renderModal();}break;
 default:return false;
 }
 return true;
}
function parityChange(input){
 if(input.dataset.facet){const field=input.dataset.facet;const target=field==='repos'?state.filterDraft:state.filterDraft.board;const selected=new Set(facetValues(target[field]));input.checked?selected.add(input.value):selected.delete(input.value);target[field]=[...selected];return true;}
 if(input.hasAttribute('data-draft-bots')){state.filterDraft.board.hideBots=input.checked;return true;}
 if(input.dataset.identityEmail){const emails=new Set(state.identityDraft.emails);input.checked?emails.add(input.dataset.identityEmail):emails.delete(input.dataset.identityEmail);state.identityDraft.emails=[...emails];if(!state.identityDraft.primaryEmail&&input.checked){state.identityDraft.primaryEmail=input.dataset.identityEmail;$('#identityPrimary').value=input.dataset.identityEmail;}return true;}
 if(input.dataset.controlRepo){const selected=new Set(state.controlsDraft);input.checked?selected.add(input.dataset.controlRepo):selected.delete(input.dataset.controlRepo);state.controlsDraft=[...selected];return true;}
 if(input.dataset.action==='select-group'){performSnapshot('SelectGroup',input.value).then(()=>{state.repos=[];state.activity.scope={repo:'',path:'',file:false};if(state.since)state.since=bookmark();render();}).catch(e=>toast(e.message));return true;}
 return false;
}
function parityKey(event){
 if(event.ctrlKey||event.metaKey||event.altKey)return false;
 const typing=['INPUT','TEXTAREA','SELECT'].includes(event.target.tagName)||event.target.isContentEditable;
 if(event.key==='F1'){event.preventDefault();state.modal==='help'?closeModal():openModal('help');return true;}
 if(typing){
  const input=event.target;const board=input.dataset.board;
  if(board&&input.dataset.field==='query'&&event.key==='Escape'){event.preventDefault();state[board].query=searchBefore[board]??state[board].query;delete searchBefore[board];render();return true;}
  if(board&&input.dataset.field==='query'&&event.key==='Enter'){event.preventDefault();delete searchBefore[board];input.blur();return true;}
  return false;
 }
 if(state.modal){if(['filters','dates'].includes(state.modal)&&event.key==='Enter'){event.preventDefault();parityAction({dataset:{action:'apply-filters'}});return true;}if(state.modal==='sort'&&/^[1-5]$/.test(event.key)){state.prs.sort=sortOptions('prs')[Number(event.key)-1][0];state.prs.reverse=false;closeModal();render();event.preventDefault();return true;}return false;}
 const consume=fn=>{event.preventDefault();fn();return true;};
 if(event.key==='f')return consume(()=>openModal('filters'));
 if(event.key==='t')return consume(()=>openModal('dates'));
 if(event.key==='e')return consume(()=>openModal('status'));
 if(event.key==='i')return consume(()=>parityAction({dataset:{action:'controls'}}).catch(e=>toast(e.message)));
 if(event.key==='x')return consume(()=>{state[state.board]=resetBoard(state[state.board]);render();});
 if(event.key==='s')return consume(()=>{const options=sortOptions(state.board);const index=options.findIndex(([key])=>key===state[state.board].sort);state[state.board].sort=options[(index+1)%options.length][0];render();});
 if(event.key==='S')return consume(()=>{state[state.board].reverse=!state[state.board].reverse;render();});
 if(event.key==='o')return consume(()=>{if(state.board==='prs'){const pr=prList().find(p=>p.repo+'#'+p.number===state.prs.selected);if(pr)openURL(pr.url);}else if(state.board==='activity'){const scope=selectedActivityScope();if(scope.repo)parityAction({dataset:{action:'open-path',repo:scope.repo,value:scope.path||''}}).catch(e=>toast(e.message));}});
 if(state.board==='prs'){
  if(['m','M','n'].includes(event.key))return consume(()=>{const mine={m:'attention',M:'authored',n:'requested'}[event.key];state.prs.mine=facetValues(state.prs.mine).length===1&&facetValues(state.prs.mine)[0]===mine?'':mine;render();});
  if(event.key==='A')return consume(()=>{state.prs.sort='author';state.prs.reverse=false;render();});
  if(event.key==='z')return consume(()=>openModal('sort'));
  if(['u','v','k','d'].includes(event.key))return consume(()=>openModal('filters'));
  if(event.key==='Escape'&&state.prContext)return consume(returnActivity);
 }
 if(state.board==='people'){
  if(event.key==='b')return consume(()=>{state.people.hideBots=!state.people.hideBots;render();});
  if(event.key==='m')return consume(()=>parityAction({dataset:{action:'identities'}}).catch(e=>toast(e.message)));
 }
 if(state.board==='activity'){
  if(/^[1-4]$/.test(event.key))return consume(()=>{state.activity.lens={1:'commits',2:'people',3:'related',4:'ranking'}[event.key];state.activity.lensPerson='';state.activity.lensEvidence=null;render();});
  if(event.key==='m'&&state.activity.lens==='related')return consume(()=>{state.activity.relatedMode=state.activity.relatedMode==='commits'?'people':'commits';render();});
  if(event.key==='v')return consume(()=>{const scope=selectedActivityScope();if(scope.repo)openContextPRs(scope.repo);});
  if(event.key==='ArrowLeft')return consume(back);
  if(event.key==='ArrowRight')return consume(()=>{const row=document.activeElement.closest('[data-row="activity"]');if(row)explore(row.dataset.id,row.dataset.kind);});
 }
 if(state.board!=='activity'&&['ArrowDown','ArrowUp'].includes(event.key))return consume(()=>{const rows=[...document.querySelectorAll(state.board==='people'?'.contributor-row':'.pr-row')];const focused=rows.indexOf(document.activeElement);const current=focused>=0?focused:rows.findIndex(r=>r.dataset.value===state[state.board].selected);const index=Math.max(0,Math.min(rows.length-1,current+(event.key==='ArrowDown'?1:-1)));if(rows[index]){const value=rows[index].dataset.value;if(state.board==='people')state.people.selected=value;else state.prs.selected=value;render();const next=[...document.querySelectorAll(state.board==='people'?'.contributor-row':'.pr-row')][index];next.focus();next.scrollIntoView({block:'nearest'});}});
 if(['PageDown','PageUp','Home','End'].includes(event.key))return consume(()=>{const rows=[...document.querySelectorAll(state.board==='activity'?'[data-row="activity"]':state.board==='people'?'.contributor-row':'.pr-row')];if(!rows.length)return;const current=rows.indexOf(document.activeElement);const index=event.key==='Home'?0:event.key==='End'?rows.length-1:Math.max(0,Math.min(rows.length-1,current+(event.key==='PageDown'?8:-8)));if(state.board==='activity')state.activity.selected=rows[index].dataset.id;else state[state.board].selected=rows[index].dataset.value;render();const next=[...document.querySelectorAll(state.board==='activity'?'[data-row="activity"]':state.board==='people'?'.contributor-row':'.pr-row')][index];next.focus();next.scrollIntoView({block:'nearest'});});
 return false;
}
document.addEventListener('focusout',event=>{const board=event.target.dataset.board;if(event.target.dataset.field==='query')setTimeout(()=>{if(document.activeElement?.dataset.field!=='query'||document.activeElement?.dataset.board!==board)delete searchBefore[board];},0);});
document.addEventListener('focusin',event=>{const input=event.target;if(input.dataset.field==='query'&&searchBefore[input.dataset.board]===undefined)searchBefore[input.dataset.board]=state[input.dataset.board].query;});
document.addEventListener('input',event=>{if(event.target.id==='identitySearch'){const query=event.target.value.toLowerCase();document.querySelectorAll('[data-identity-row]').forEach(row=>row.hidden=!row.textContent.toLowerCase().includes(query));}});

async function start(){
 try{state.snapshot=validSnapshot(await api('Snapshot'));applyPreferences();prEvidenceLoaded=state.snapshot.mode==='live'&&(state.snapshot.status.phase==='ready'||state.snapshot.pullRequests.length>0);render();if(!dateValue(state.snapshot.generatedAt)||state.snapshot.status.phase==='initial')await refresh();}
 catch(e){$('#app').innerHTML=`<div class="boot"><div class="brand-mark">▥</div><h1>Unable to open the workspace</h1><p>${esc(e.message)}</p><button class="button primary" id="retry-start">Try again</button></div>`;$('#retry-start').addEventListener('click',start);}
}
start();
