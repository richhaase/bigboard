export const ranges = [{value:7,label:'7 days'},{value:30,label:'30 days'},{value:90,label:'90 days'},{value:365,label:'1 year'},{value:0,label:'All time'}];
export const number = value => new Intl.NumberFormat('en-US').format(value || 0);
export const compact = value => new Intl.NumberFormat('en-US', {notation:'compact',maximumFractionDigits:1}).format(value || 0);
export const identity = c => c.email || `${c.repo}:${c.author}`;
export const change = c => (c.added || 0) + (c.removed || 0);
export const dateValue = value => { const time = typeof value === 'number' ? value : /^0001-01-01T/.test(value) ? 0 : Date.parse(value); return Number.isFinite(time) ? time : 0; };
export function inRange(date, days, now = Date.now()) { return !days || (dateValue(date) > 0 && dateValue(date) >= now - days * 86400000); }
export function selectCommits(snapshot, filters = {}, now = Date.now()) {
  return (snapshot.commits || []).filter(c => (!filters.repos?.length || filters.repos.includes(c.repo)) && inRange(c.date, filters.days, now) && (!filters.hideBots || !c.bot));
}
export function uniqueCommits(commits) {
  const result = new Map();
  for (const c of commits) { const key = c.id || `${c.repo}:${c.date}:${identity(c)}:${c.subject}`; const old = result.get(key); if (!old || (old.linesUnknown && !c.linesUnknown) || (!!old.linesUnknown === !!c.linesUnknown && change(c) > change(old))) result.set(key,c); }
  return [...result.values()];
}
export function totals(commits) {
  const items = uniqueCommits(commits);
  return {commits:items.length,added:items.reduce((n,c)=>n+(c.linesUnknown?0:c.added||0),0),removed:items.reduce((n,c)=>n+(c.linesUnknown?0:c.removed||0),0),people:new Set(items.map(identity)).size,ai:items.filter(c=>c.ai).length,unknown:items.some(c=>c.linesUnknown)};
}
export function people(commits) {
  const grouped = new Map();
  for (const c of commits) { const id = identity(c); if (!grouped.has(id)) grouped.set(id,{id,name:c.author || c.email || 'Unknown contributor',email:c.email,bot:c.bot,records:[]}); grouped.get(id).records.push(c); }
  return [...grouped.values()].map(p=>({...p,...totals(p.records),latest:Math.max(...p.records.map(c=>dateValue(c.date))),repos:[...new Set(p.records.map(c=>c.repo))]})).sort((a,b)=>(b.added+b.removed)-(a.added+a.removed)||a.name.localeCompare(b.name));
}
export function scopedCommits(commits, scope) {
  if (!scope.repo) return commits;
  const selected = commits.filter(c=>c.repo===scope.repo);
  if (!scope.path) return selected;
  return selected.flatMap(c=>{
    const files=(c.files||[]).filter(f=>f.path===scope.path||f.path.startsWith(`${scope.path}/`));
    return files.length ? [{...c,files,added:files.reduce((n,f)=>n+(f.added||0),0),removed:files.reduce((n,f)=>n+(f.removed||0),0)}] : [];
  });
}
export function activityRows(commits, repositories, scope={}) {
  if (!scope.repo) return repositories.map(repo=>{const records=commits.filter(c=>c.repo===repo.id); return {...repo,label:repo.name || repo.id.split('/').pop(),kind:'repository',records,...totals(records),latest:Math.max(0,...records.map(c=>dateValue(c.date)))};});
  const grouped=new Map(); const prefix=scope.path ? `${scope.path}/` : '';
  for (const c of commits.filter(c=>c.repo===scope.repo)) {
    for (const file of c.files||[]) {
      if (!file.path.startsWith(prefix)) continue;
      const remainder=file.path.slice(prefix.length); const first=remainder.split('/')[0]; if (!first) continue;
      const path=prefix+first; const kind=remainder.includes('/')?'directory':'file';
      if (!grouped.has(path)) grouped.set(path,{id:path,label:first,path,kind,records:new Map(),added:0,removed:0});
      const row=grouped.get(path); row.added+=c.linesUnknown?0:file.added||0; row.removed+=c.linesUnknown?0:file.removed||0; row.records.set(c.id,c);
    }
  }
  return [...grouped.values()].map(row=>{const records=[...row.records.values()]; return {...row,records,commits:records.length,people:new Set(records.map(identity)).size,latest:Math.max(0,...records.map(c=>dateValue(c.date))),unknown:records.some(c=>c.linesUnknown)};});
}
export function sortRows(rows, sort='change', reverse=false) {
  const compare={change:(a,b)=>(b.added+b.removed)-(a.added+a.removed),commits:(a,b)=>b.commits-a.commits,name:(a,b)=>(a.label||a.name).localeCompare(b.label||b.name),recent:(a,b)=>b.latest-a.latest}[sort]||(()=>0);
  return [...rows].sort((a,b)=>(compare(a,b)||(a.label||a.name||'').localeCompare(b.label||b.name||''))*(reverse?-1:1));
}
export function dailySeries(commits, days=30, now = Date.now()) {
  const duration=Math.min(days||365,365); const cutoff=now-duration*86400000;
  const start=new Date(cutoff); start.setHours(0,0,0,0);
  const end=new Date(now); end.setHours(0,0,0,0);
  const rows=[]; const byDay=new Map();
  for(const d=new Date(start);d<=end;d.setDate(d.getDate()+1)){
    const key=`${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
    const point={date:d.toISOString(),commits:0,added:0,removed:0};rows.push(point);byDay.set(key,point);
  }
  for(const c of uniqueCommits(commits)){
    if(dateValue(c.date)<cutoff)continue;
    const d=new Date(c.date);const point=byDay.get(`${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`);
    if(point){point.commits++;point.added+=c.linesUnknown?0:c.added||0;point.removed+=c.linesUnknown?0:c.removed||0;}
  }
  return rows;
}
export function filterPRs(snapshot, filters={}, now = Date.now()) {
  const query=(filters.query||'').toLowerCase();
  const selected=(snapshot.pullRequests||[]).filter(p=>(!filters.repos?.length||filters.repos.includes(p.repo))&&inRange(p.updatedAt,filters.days,now)&&(!query||`${p.repo} ${p.title} ${p.author} ${p.number}`.toLowerCase().includes(query))&&(!filters.author||p.author===filters.author)&&(!filters.review||p.review===filters.review)&&(!filters.checks||p.checks===filters.checks)&&(!filters.state||(filters.state==='draft'?p.draft:!p.draft))&&(!filters.mine||(snapshot.viewer&&(filters.mine==='authored'?p.author===snapshot.viewer:(p.reviewers||[]).includes(snapshot.viewer)))));
  const priority=p=>(p.reviewers||[]).includes(snapshot.viewer)&&snapshot.viewer?0:['FAILURE','ERROR','FAILING'].includes(p.checks)?1:p.review==='CHANGES_REQUESTED'?2:p.review==='APPROVED'?3:4;
  return selected.sort((a,b)=>filters.sort==='attention'?priority(a)-priority(b)||(dateValue(a.updatedAt)||Infinity)-(dateValue(b.updatedAt)||Infinity):filters.sort==='repository'?a.repo.localeCompare(b.repo)||b.number-a.number:dateValue(b.updatedAt)-dateValue(a.updatedAt));
}
export function safeURL(url) {
  try {const u=new URL(url);return u.protocol==='https:'&&u.host==='github.com'&&!u.username&&!u.password ? u.href : null;} catch {return null;}
}
