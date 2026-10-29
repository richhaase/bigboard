export const ranges = [{value:1,label:'1 day'},{value:7,label:'7 days'},{value:14,label:'14 days'},{value:30,label:'30 days'},{value:90,label:'90 days'},{value:365,label:'1 year'},{value:0,label:'All time'}];
export const number = value => new Intl.NumberFormat('en-US').format(value || 0);
export const compact = value => new Intl.NumberFormat('en-US', {notation:'compact',maximumFractionDigits:1}).format(value || 0);
export const identity = c => c.email?.trim().toLowerCase() || `${c.repo}:${c.author}`;
export const change = c => (c.added || 0) + (c.removed || 0);
export const dateValue = value => { const time = typeof value === 'number' ? value : /^0001-01-01T/.test(value) ? 0 : Date.parse(value); return Number.isFinite(time) ? time : 0; };
export function inRange(date, days, now = Date.now(), since = '') { if(since)return dateValue(date)>dateValue(since); return !days || (dateValue(date) > 0 && dateValue(date) > now - days * 86400000); }
export function selectCommits(snapshot, filters = {}, now = Date.now()) {
  const records=(snapshot.commits || []).filter(c => (!filters.repos?.length || filters.repos.includes(c.repo)) && inRange(c.date, filters.days, now, filters.since));
  const selected=uniqueCommits(records);const canonical=new Map(selected.filter(c=>c.id).map(c=>[c.id,c]));const bots=new Set(selected.filter(c=>c.bot).map(identity));
  return records.filter(c=>{const preferred=canonical.get(c.id)||c;return (!filters.hideBots||!bots.has(identity(preferred)))&&(!filters.people?.length||filters.people.includes(identity(preferred)));});
}
export function uniqueCommits(commits) {
  const result=[];const indices=new Map();
  const key=c=>(c.email?.trim()?'email:'+identity(c):'missing:'+JSON.stringify(c.repo)+':'+JSON.stringify(c.author))+'\0'+c.author;
  for(const c of commits){
    if(!c.id||!indices.has(c.id)){if(c.id)indices.set(c.id,result.length);result.push(c);continue;}
    const index=indices.get(c.id);const old=result[index];
    if((old.linesUnknown&&!c.linesUnknown)||(!!old.linesUnknown===!!c.linesUnknown&&(c.repo<old.repo||(c.repo===old.repo&&key(c)<key(old)))))result[index]=c;
  }
  return result;
}
export function totals(commits) {
  const items = uniqueCommits(commits);
  return {commits:items.length,added:items.reduce((n,c)=>n+(c.linesUnknown?0:c.added||0),0),removed:items.reduce((n,c)=>n+(c.linesUnknown?0:c.removed||0),0),people:new Set(items.map(identity)).size,ai:items.filter(c=>c.ai).length,unknown:items.some(c=>c.linesUnknown)};
}
export function people(commits) {
  const canonical=new Map(uniqueCommits(commits).filter(c=>c.id).map(c=>[c.id,c]));
  const grouped=new Map();
  for(const c of commits){
    const preferred=canonical.get(c.id)||c;const id=identity(preferred);
    if(!grouped.has(id))grouped.set(id,{id,name:preferred.author||preferred.email||'Unknown contributor',email:preferred.email,bot:preferred.bot,records:[]});
    grouped.get(id).bot=grouped.get(id).bot||preferred.bot;
    grouped.get(id).records.push({...c,author:preferred.author,email:preferred.email,added:preferred.added,removed:preferred.removed,ai:preferred.ai,bot:preferred.bot,linesUnknown:preferred.linesUnknown});
  }
  return [...grouped.values()].map(p=>{const names=new Map();for(const c of uniqueCommits(p.records))names.set(c.author,(names.get(c.author)||0)+1);const name=[...names].sort((a,b)=>b[1]-a[1]||[...b[0]].length-[...a[0]].length||(a[0]<b[0]?-1:a[0]>b[0]?1:0))[0]?.[0]||p.name;return {...p,name,...totals(p.records),latest:p.records.reduce((latest,c)=>Math.max(latest,dateValue(c.date)),0),repos:[...new Set(p.records.map(c=>c.repo))]};}).sort((a,b)=>(b.added+b.removed)-(a.added+a.removed)||b.commits-a.commits||a.name.localeCompare(b.name));
}
export function scopedCommits(commits, scope) {
  if (!scope.repo) return commits;
  const selected = commits.filter(c=>c.repo===scope.repo);
  if (!scope.path&&!scope.areaName) return selected;
  return selected.flatMap(c=>{
    const files=(c.files||[]).filter(f=>scope.areaName?classifyWorkArea(f.path,scope.areaRules||[])===scope.areaName:f.path===scope.path||f.path.startsWith(`${scope.path}/`));
    return files.length ? [{...c,files,added:files.reduce((n,f)=>n+(f.added||0),0),removed:files.reduce((n,f)=>n+(f.removed||0),0)}] : [];
  });
}
export function activityRows(commits, repositories, scope={}) {
  if (!scope.repo) {
    const recordsByRepo=new Map();
    for(const commit of commits){if(!recordsByRepo.has(commit.repo))recordsByRepo.set(commit.repo,[]);recordsByRepo.get(commit.repo).push(commit);}
    return repositories.map(repo=>{const records=recordsByRepo.get(repo.id)||[];return {...repo,label:repo.name || repo.id.split('/').pop(),kind:'repository',records,...totals(records),latest:records.reduce((latest,c)=>Math.max(latest,dateValue(c.date)),0)};});
  }
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
  return [...grouped.values()].map(row=>{const records=[...row.records.values()]; return {...row,records,commits:records.length,people:new Set(records.map(identity)).size,latest:records.reduce((latest,c)=>Math.max(latest,dateValue(c.date)),0),unknown:records.some(c=>c.linesUnknown)};});
}
export function sortRows(rows, sort='change', reverse=false) {
  const compare={change:(a,b)=>(b.added+b.removed)-(a.added+a.removed),commits:(a,b)=>b.commits-a.commits,added:(a,b)=>b.added-a.added,removed:(a,b)=>b.removed-a.removed,net:(a,b)=>(b.added-b.removed)-(a.added-a.removed),ai:(a,b)=>(b.ai/Math.max(1,b.commits))-(a.ai/Math.max(1,a.commits)),name:(a,b)=>(a.label||a.name).localeCompare(b.label||b.name),recent:(a,b)=>b.latest-a.latest}[sort]||(()=>0);
  return [...rows].sort((a,b)=>(compare(a,b)||((!a.label&&!b.label)?((b.added+b.removed)-(a.added+a.removed)||b.commits-a.commits):0)||((a.label||a.name||'')<(b.label||b.name||'')?-1:(a.label||a.name||'')>(b.label||b.name||'')?1:0))*(reverse?-1:1));
}
export function dailySeries(commits, days=30, now = Date.now()) {
  const duration=days||365; const cutoff=now-duration*86400000;
  const start=new Date(cutoff); start.setHours(0,0,0,0);
  const end=new Date(now); end.setHours(0,0,0,0);
  const rows=[]; const byDay=new Map();
  for(const d=new Date(start);d<=end;d.setDate(d.getDate()+1)){
    const key=`${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
    const point={date:d.toISOString(),commits:0,added:0,removed:0};rows.push(point);byDay.set(key,point);
  }
  for(const c of uniqueCommits(commits)){
    if(dateValue(c.date)<cutoff)continue;
    const d=new Date(c.date);const key=`${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;let point=byDay.get(key);if(!point&&dateValue(c.date)>now){const date=new Date(d);date.setHours(0,0,0,0);point={date:date.toISOString(),commits:0,added:0,removed:0};byDay.set(key,point);rows.push(point);}
    if(point){point.commits++;point.added+=c.linesUnknown?0:c.added||0;point.removed+=c.linesUnknown?0:c.removed||0;}
  }
  return rows.sort((a,b)=>dateValue(a.date)-dateValue(b.date));
}
export const facetValues=value=>Array.isArray(value)?value:value?[value]:[];
export function updatedSinceReview(pr,viewer){
  if(!viewer)return false;
  const reviewed=(pr.reviews||[]).filter(r=>r.author.toLowerCase()===viewer.toLowerCase()).reduce((latest,r)=>Math.max(latest,dateValue(r.submittedAt)),0);
  return reviewed>0&&dateValue(pr.updatedAt)>reviewed;
}
export function needsAttention(pr,viewer){
  if(!viewer)return false;
  const owned=pr.author.toLowerCase()===viewer.toLowerCase();
  return (!owned&&(pr.reviewers||[]).some(r=>!r.includes('/')&&r.toLowerCase()===viewer.toLowerCase()))||(owned&&(['FAILURE','ERROR','FAILING'].includes(pr.checks)||pr.review==='CHANGES_REQUESTED'||(!pr.draft&&pr.review==='APPROVED'&&pr.checks==='SUCCESS')))||(!owned&&updatedSinceReview(pr,viewer));
}
export function filterPRs(snapshot, filters={}, now = Date.now()) {
  const query=(filters.query||'').toLowerCase();
  const matches=(facet,value)=>!facetValues(facet).length||facetValues(facet).includes(value);
  const mine=facetValues(filters.mine);
  const selected=(snapshot.pullRequests||[]).filter(p=>(!filters.repos?.length||filters.repos.includes(p.repo))&&inRange(p.updatedAt,filters.days,now,filters.since)&&(!query||`${p.repo} ${p.title} ${p.author} ${p.number}`.toLowerCase().includes(query))&&matches(facetValues(filters.author).map(a=>a.toLowerCase()),p.author.toLowerCase())&&matches(filters.review,p.review)&&matches(filters.checks,p.checks)&&matches(filters.state,p.draft?'draft':'open')&&(!mine.length||(snapshot.viewer&&mine.some(value=>value==='authored'?p.author.toLowerCase()===(snapshot.viewer||'').toLowerCase():value==='requested'?(p.reviewers||[]).some(r=>!r.includes('/')&&r.toLowerCase()===(snapshot.viewer||'').toLowerCase()):value==='updated'?updatedSinceReview(p,snapshot.viewer):value==='attention'?needsAttention(p,snapshot.viewer):false))));
  const priority=p=>(p.reviewers||[]).some(r=>!r.includes('/')&&r.toLowerCase()===(snapshot.viewer||'').toLowerCase())&&snapshot.viewer?0:['FAILURE','ERROR','FAILING'].includes(p.checks)?1:p.review==='CHANGES_REQUESTED'?2:p.review==='APPROVED'?3:4;
  const newest=(a,b)=>dateValue(b.updatedAt)-dateValue(a.updatedAt)||a.repo.localeCompare(b.repo)||b.number-a.number;
  const compare=(a,b)=>filters.sort==='attention'?priority(a)-priority(b)||(dateValue(a.updatedAt)||Infinity)-(dateValue(b.updatedAt)||Infinity)||a.repo.localeCompare(b.repo)||b.number-a.number:filters.sort==='repository'?a.repo.localeCompare(b.repo)||newest(a,b):filters.sort==='author'?a.author.toLowerCase().localeCompare(b.author.toLowerCase())||newest(a,b):filters.sort==='title'?a.title.localeCompare(b.title)||newest(a,b):newest(a,b);
  return selected.sort((a,b)=>compare(a,b)*(filters.reverse?-1:1));
}
export function timelineSeries(commits,days=30,now=Date.now()){
  if(days>0&&days<=30)return dailySeries(commits,days,now);
  const weekly=days>0&&days<=180;
  const start=new Date(now);start.setHours(0,0,0,0);
  if(weekly){start.setDate(start.getDate()-days);start.setDate(start.getDate()-start.getDay());}
  else{start.setDate(1);start.setMonth(start.getMonth()-11);}
  const rows=[];
  for(const date=new Date(start);date<=new Date(now);weekly?date.setDate(date.getDate()+7):date.setMonth(date.getMonth()+1))rows.push({date:date.toISOString(),commits:0,added:0,removed:0});
  const key=date=>weekly?new Date(date.getFullYear(),date.getMonth(),date.getDate()-date.getDay()).getTime():new Date(date.getFullYear(),date.getMonth(),1).getTime();
  const byDate=new Map(rows.map(r=>[key(new Date(r.date)),r]));
  for(const c of uniqueCommits(commits)){const bucket=key(new Date(c.date));let row=byDate.get(bucket);if(!row&&dateValue(c.date)>now){row={date:new Date(bucket).toISOString(),commits:0,added:0,removed:0};byDate.set(bucket,row);rows.push(row);}if(row){row.commits++;row.added+=c.linesUnknown?0:c.added||0;row.removed+=c.linesUnknown?0:c.removed||0;}}
  return rows.sort((a,b)=>dateValue(a.date)-dateValue(b.date));
}
export function classifyWorkArea(path,rules){
  let chosen='',length=-1;
  for(const rule of rules)for(const raw of rule.paths||[]){const prefix=raw.replace(/\/$/,'');if((path===prefix||path.startsWith(prefix+'/'))&&prefix.length>length){chosen=rule.name;length=prefix.length;}}
  return chosen;
}
export function relatedScopes(commits,scope,mode='commits',workAreas={}){
  const source=scopedCommits(commits,scope);
  const keys=new Set(source.map(c=>mode==='people'?identity(c):c.id).filter(Boolean));
  const grouped=new Map();
  for(const c of commits){
    const evidence=mode==='people'?identity(c):c.id;
    if(!keys.has(evidence))continue;
    const targets=scope.path?new Set((c.files||[]).map(f=>({repo:c.repo,path:f.path.includes('/')?f.path.split('/').slice(0,-1).join('/'):'',file:false})).map(s=>JSON.stringify(s))):new Set([JSON.stringify({repo:c.repo,path:'',file:false})]);
    for(const file of c.files||[]){const areaName=classifyWorkArea(file.path,workAreas[c.repo]||[]);if(areaName&&!(scope.repo===c.repo&&scope.areaName===areaName))targets.add(JSON.stringify({repo:c.repo,path:'',file:false,areaName}));}
    for(const raw of targets){const target=JSON.parse(raw);if(!target.areaName&&target.repo===scope.repo&&(target.path===scope.path||!scope.path)&&!scope.areaName)continue;if(!grouped.has(raw))grouped.set(raw,{...target,evidence:new Set(),records:[]});const row=grouped.get(raw);row.evidence.add(evidence);row.records.push(c);}
  }
  return [...grouped.values()].map(r=>({...r,count:r.evidence.size,evidence:[...r.evidence]})).sort((a,b)=>b.count-a.count||a.repo.localeCompare(b.repo)||a.path.localeCompare(b.path));
}
export function safeURL(url) {
  try {const u=new URL(url);return u.protocol==='https:'&&u.host==='github.com'&&!u.username&&!u.password ? u.href : null;} catch {return null;}
}
