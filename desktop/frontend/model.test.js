import test from 'node:test';
import assert from 'node:assert/strict';
import {
  activityRows, dailySeries, dateValue, filterPRs, inRange, people, safeURL, ranges, identity, timelineSeries, relatedScopes, classifyWorkArea,
  scopedCommits, selectCommits, sortRows, totals, uniqueCommits, filterByCanonicalIdentity, excludedLines,
  HEAT, heatLevel, heatSeries, heatRows,
} from './model.js';

const now = Date.parse('2026-10-28T12:00:00Z');
const makeCommit = (overrides = {}) => ({
  id: 'abc', repo: 'sample/api', author: 'Alex', email: 'alex@example.test',
  date: '2026-10-27T12:00:00Z', subject: 'Improve queue handling',
  added: 120, removed: 30, ai: false, bot: false, linesUnknown: false,
  files: [{path:'src/queue.go',added:80,removed:20},{path:'docs/queue.md',added:40,removed:10}],
  ...overrides,
});
const makePR = (overrides = {}) => ({
  repo:'sample/api', number:1, title:'Improve queue handling', author:'alex',
  updatedAt:'2026-10-27T12:00:00Z', review:'REVIEW_REQUIRED', checks:'SUCCESS',
  reviewers:[], draft:false, ...overrides,
});

test('date filtering retains future commits and requires known dates for bounded ranges', () => {
  assert.equal(inRange('2026-10-21T12:00:00Z',7,now),false);
  assert.equal(inRange('2026-10-21T12:00:00.001Z',7,now),true);
  assert.equal(inRange('2026-10-21T11:59:59Z',7,now),false);
  assert.equal(inRange('2027-01-01T00:00:00Z',7,now),true);
  assert.equal(inRange('not a date',7,now),false);
  assert.equal(inRange('',0,now),true);
});

test('repository, date, and bot selection compose before global commit deduplication', () => {
  const snapshot = {commits:[
    makeCommit({repo:'sample/api'}),
    makeCommit({repo:'sample/web'}),
    makeCommit({id:'old',date:'2026-01-01T00:00:00Z'}),
    makeCommit({id:'bot',bot:true,email:'automation@example.test'}),
  ]};
  const selected = selectCommits(snapshot,{repos:['sample/web'],days:7,hideBots:true},now);
  assert.equal(selected.length,1);
  assert.equal(selected[0].repo,'sample/web');
  assert.equal(totals(selectCommits(snapshot,{days:7,hideBots:true},now)).commits,1);
});

test('hiding bots excludes every selected commit of an identity marked as a bot', () => {
  const snapshot={commits:[
    makeCommit({id:'bot',bot:true}),
    makeCommit({id:'human',bot:false}),
    makeCommit({id:'other',email:'human@example.test'}),
  ]};
  assert.deepEqual(selectCommits(snapshot,{days:7,hideBots:true},now).map(c=>c.id),['other']);
  assert.equal(people(snapshot.commits).find(p=>p.email==='alex@example.test').bot,true);
});

test('canonical contributor names use unique-commit frequency then longest name and codepoint order', () => {
  const frequent=[makeCommit({id:'one',author:'Alex'}),makeCommit({id:'two',author:'Alex'}),makeCommit({id:'three',author:'Alex Example'})];
  assert.equal(people(frequent)[0].name,'Alex');
  assert.equal(people([makeCommit({id:'one',author:'Alex'}),makeCommit({id:'two',author:'Alex Example'})])[0].name,'Alex Example');
  assert.equal(people([makeCommit({id:'one',author:'Blex'}),makeCommit({id:'two',author:'Alex'})])[0].name,'Alex');
  assert.equal(people([...frequent,makeCommit({id:'three',author:'Alex Example',repo:'sample/web'}),makeCommit({id:'three',author:'Alex Example',repo:'sample/tools'})])[0].name,'Alex');
});

test('deduplication always prefers known counts regardless of record order or magnitude', () => {
  const known = makeCommit({added:5,removed:2});
  const unknown = makeCommit({added:900,removed:900,linesUnknown:true});
  assert.deepEqual(uniqueCommits([known,unknown]),[known]);
  assert.deepEqual(uniqueCommits([unknown,known]),[known]);
});

test('contributor grouping uses identities rather than display names', () => {
  const contributors=people([
    makeCommit({id:'one',author:'Alex',email:'first@example.test'}),
    makeCommit({id:'two',author:'Alex',email:'second@example.test'}),
    makeCommit({id:'three',author:'A. Example',email:'first@example.test'}),
  ]);
  assert.equal(contributors.length,2);
  assert.equal(contributors.find(p=>p.email==='first@example.test').commits,2);
  assert.equal(contributors.find(p=>p.email==='second@example.test').commits,1);
});

test('missing-email contributors remain repository scoped', () => {
  const contributors=people([
    makeCommit({id:'one',email:'',repo:'sample/api'}),
    makeCommit({id:'two',email:'',repo:'sample/web'}),
  ]);
  assert.equal(contributors.length,2);
});

test('shared commits count once globally while contributor repository membership stays complete', () => {
  const records=[makeCommit(),makeCommit({repo:'sample/web'})];
  const contributor=people(records)[0];
  assert.equal(contributor.commits,1);
  assert.deepEqual([...contributor.repos].sort(),['sample/api','sample/web']);
  const repositories=[{id:'sample/api',name:'api'},{id:'sample/web',name:'web'},{id:'sample/quiet',name:'quiet'}];
  const rows=activityRows(records,repositories);
  assert.deepEqual(rows.map(r=>r.commits),[1,1,0]);
});

test('directory rankings attribute only child file changes and count a commit once per directory', () => {
  const records=[makeCommit({files:[
    {path:'src/queue.go',added:60,removed:20},
    {path:'src/worker.go',added:20,removed:0},
    {path:'docs/queue.md',added:40,removed:10},
  ]})];
  const rows=activityRows(records,[],{repo:'sample/api'});
  const src=rows.find(r=>r.path==='src');
  const docs=rows.find(r=>r.path==='docs');
  assert.equal(src.kind,'directory');
  assert.equal(src.commits,1);
  assert.equal(src.added,80);
  assert.equal(src.removed,20);
  assert.equal(docs.added,40);
  assert.equal(docs.removed,10);
  const files=activityRows(records,[],{repo:'sample/api',path:'src'});
  assert.deepEqual(files.map(r=>r.path).sort(),['src/queue.go','src/worker.go']);
  assert.ok(files.every(r=>r.kind==='file'));
});

test('scope selection respects path component boundaries and narrows contribution totals', () => {
  const records=[makeCommit({files:[
    {path:'src/queue.go',added:80,removed:20},
    {path:'src-old/queue.go',added:400,removed:100},
    {path:'docs/queue.md',added:40,removed:10},
  ]}),makeCommit({id:'other',repo:'sample/web'})];
  const scoped=scopedCommits(records,{repo:'sample/api',path:'src'});
  assert.equal(scoped.length,1);
  assert.equal(scoped[0].added,80);
  assert.equal(scoped[0].removed,20);
  assert.deepEqual(scoped[0].files.map(f=>f.path),['src/queue.go']);
  assert.equal(records[0].files.length,3);
  assert.equal(scopedCommits(records,{repo:'sample/api',path:'missing'}).length,0);
});

test('directory rankings retain every id-less record while deduplicating identified copies', () => {
  const missing=makeCommit({id:'',files:[{path:'src/one.go',added:4,removed:1},{path:'src/two.go',added:6,removed:2}]});
  const other={...missing,id:undefined,author:'Taylor',email:'taylor@example.test'};
  const identified={...missing,id:'known'};
  const records=[missing,missing,other,identified,identified];
  const [directory]=activityRows(records,[],{repo:'sample/api'});
  assert.equal(directory.commits,4);
  assert.equal(directory.records.length,4);
  assert.equal(directory.people,2);
  assert.equal(directory.added,40);
  assert.equal(directory.removed,12);
  const files=activityRows(records,[],{repo:'sample/api',path:'src'});
  assert.ok(files.every(row=>row.commits===4));
  assert.equal(totals(scopedCommits(records,{repo:'sample/api',path:'src'})).commits,4);
});

test('unknown paths remain repository evidence without inventing directory associations', () => {
  const unknown=makeCommit({id:'unknown-paths',pathsUnknown:true,linesUnknown:true,files:[]});
  const unknownWithUntrustedFiles={...unknown,id:'unknown-with-files',files:[{path:'src/guessed.go',added:999,removed:999}]};
  const knownPath=makeCommit({id:'unknown-lines',linesUnknown:true,files:[{path:'src/known.go',added:999,removed:999}]});
  const records=[unknown,unknownWithUntrustedFiles,knownPath];
  const repository=scopedCommits(records,{repo:'sample/api'});
  assert.equal(totals(repository).commits,3);
  assert.equal(totals(repository).unknown,true);
  const scoped=scopedCommits(records,{repo:'sample/api',path:'src'});
  assert.deepEqual(scoped.map(c=>c.id),['unknown-lines']);
  const [directory]=activityRows(records,[],{repo:'sample/api'});
  assert.equal(directory.commits,1);
  assert.equal(directory.unknown,true);
  assert.equal(directory.added,0);
  assert.equal(directory.removed,0);
  assert.equal(activityRows([unknown,unknownWithUntrustedFiles],[],{repo:'sample/api'}).length,0);
});

test('daily series fills quiet calendar days and deduplicates shared commits', () => {
  const localNoon=new Date(2026,9,28,12);
  const record=makeCommit({date:localNoon.toISOString(),added:9,removed:3});
  const series=dailySeries([record,{...record,repo:'sample/web'}],7,localNoon.getTime());
  assert.equal(series.length,8);
  assert.equal(series.at(-1).commits,1);
  assert.equal(series.at(-1).added,9);
  assert.equal(series.filter(day=>day.commits===0).length,7);
});

test('row sorting is deterministic, reversible, and leaves inputs untouched', () => {
  const rows=[{label:'Beta',added:4,removed:1,commits:1,latest:1},{label:'Alpha',added:2,removed:3,commits:2,latest:2}];
  assert.deepEqual(sortRows(rows).map(r=>r.label),['Alpha','Beta']);
  assert.deepEqual(sortRows(rows,'change',true).map(r=>r.label),['Beta','Alpha']);
  assert.deepEqual(sortRows(rows,'commits').map(r=>r.label),['Alpha','Beta']);
  assert.equal(rows[0].label,'Beta');
});

test('PR filters combine shared scope and per-board state while matching review requests to viewer', () => {
  const snapshot={viewer:'rich',pullRequests:[
    makePR({number:1,reviewers:['rich'],checks:'FAILURE',draft:true}),
    makePR({number:2,reviewers:['rich'],repo:'sample/web',checks:'FAILURE',draft:true}),
    makePR({number:3,reviewers:['other'],checks:'FAILURE',draft:true}),
    makePR({number:4,reviewers:['rich'],checks:'SUCCESS'}),
  ]};
  assert.deepEqual(filterPRs(snapshot,{repos:['sample/api'],days:7,mine:'requested',checks:'FAILURE',state:'draft',query:'queue'},now).map(p=>p.number),[1]);
  assert.equal(filterPRs({...snapshot,viewer:''},{mine:'requested'},now).length,0);
});

test('PR attention puts requested reviews ahead of failed checks and changes requested', () => {
  const snapshot={viewer:'rich',pullRequests:[
    makePR({number:5}),
    makePR({number:4,review:'APPROVED'}),
    makePR({number:3,review:'CHANGES_REQUESTED'}),
    makePR({number:2,checks:'FAILURE'}),
    makePR({number:1,reviewers:['rich']}),
  ]};
  assert.deepEqual(filterPRs(snapshot,{sort:'attention'},now).map(p=>p.number),[1,2,3,4,5]);
});

test('external links accept HTTPS GitHub URLs and reject credentials or executable schemes', () => {
  assert.equal(safeURL('https://github.com/sample/api/pull/1'),'https://github.com/sample/api/pull/1');
  for(const value of ['javascript:alert(1)','http://github.com/sample/api','https://github.com.evil.test/a','https://user:secret@github.com/sample/api']) assert.equal(safeURL(value),null);
});

test('PR attention places unknown update times after known dates within each priority', () => {
  const snapshot={viewer:'rich',pullRequests:[
    makePR({number:1,updatedAt:''}),
    makePR({number:2,updatedAt:'2026-10-22T12:00:00Z'}),
    makePR({number:3,updatedAt:'2026-10-25T12:00:00Z'}),
  ]};
  assert.deepEqual(filterPRs(snapshot,{sort:'attention'},now).map(p=>p.number),[2,3,1]);
});

test('timeline includes the partial calendar day at the rolling range boundary', () => {
  const localNoon=new Date(2026,9,28,12);
  const boundary=new Date(localNoon);
  boundary.setDate(boundary.getDate()-7);
  boundary.setHours(13);
  const record=makeCommit({date:boundary.toISOString()});
  const selected=selectCommits({commits:[record]},{days:7},localNoon.getTime());
  assert.equal(selected.length,1);
  const series=dailySeries(selected,7,localNoon.getTime());
  assert.equal(series.reduce((n,day)=>n+day.commits,0),1);
});


test('timestamp handling accepts numeric projection timestamps and treats Go zero time as unknown', () => {
  assert.equal(dateValue(now),now);
  assert.equal(dateValue('0001-01-01T00:00:00Z'),0);
  assert.equal(dateValue('not a date'),0);
});

test('unknown line counts do not become measured volume in totals or the timeline', () => {
  const localNoon=new Date(2026,9,28,12);
  const records=[
    makeCommit({id:'known',date:localNoon.toISOString(),added:9,removed:3}),
    makeCommit({id:'unknown',date:localNoon.toISOString(),added:400,removed:100,linesUnknown:true}),
  ];
  const aggregate=totals(records);
  assert.equal(aggregate.commits,2);
  assert.equal(aggregate.added,9);
  assert.equal(aggregate.removed,3);
  assert.equal(aggregate.unknown,true);
  const today=dailySeries(records,7,localNoon.getTime()).at(-1);
  assert.equal(today.commits,2);
  assert.equal(today.added,9);
  assert.equal(today.removed,3);
});

let largeHistory;
function historyBeyondArgumentLimit() {
  if (!largeHistory) {
    const base=makeCommit();
    largeHistory=Array.from({length:160001},(_,index)=>({...base,id:`history-${index}`}));
    largeHistory[largeHistory.length-1]={...largeHistory.at(-1),date:'2026-10-28T12:00:00Z'};
  }
  return largeHistory;
}

test('repository activity supports histories larger than the JavaScript argument limit', () => {
  const history=historyBeyondArgumentLimit();
  const [repository]=activityRows(history,[{id:'sample/api',name:'api'}]);
  assert.equal(repository.commits,history.length);
  assert.equal(repository.latest,now);
  assert.equal(repository.added,history.length*120);
  assert.equal(repository.removed,history.length*30);
});

test('contributor aggregation supports one contributor with more than 150k commits', () => {
  const history=historyBeyondArgumentLimit();
  const contributors=people(history);
  assert.equal(contributors.length,1);
  assert.equal(contributors[0].commits,history.length);
  assert.equal(contributors[0].latest,now);
  assert.deepEqual(contributors[0].repos,['sample/api']);
});

test('directory activity supports a path changed by more than 150k commits', () => {
  const history=historyBeyondArgumentLimit();
  const rows=activityRows(history,[],{repo:'sample/api'});
  assert.equal(rows.length,2);
  const source=rows.find(row=>row.path==='src');
  assert.equal(source.commits,history.length);
  assert.equal(source.latest,now);
  assert.equal(source.added,history.length*80);
  assert.equal(source.removed,history.length*20);
});

test('commit identity matches normalized email semantics', () => {
  const contributors=people([
    makeCommit({id:'one',email:' Alex@Example.Test '}),
    makeCommit({id:'two',email:'alex@example.test'}),
  ]);
  assert.equal(contributors.length,1);
  assert.equal(contributors[0].commits,2);
});

test('same-commit attribution follows deterministic complete repository copy rather than largest volume', () => {
  const first=makeCommit({repo:'example/a',email:'canonical@example.test',added:3,removed:1});
  const second=makeCommit({repo:'example/z',email:'other@example.test',added:300,removed:100});
  assert.deepEqual(uniqueCommits([second,first]),[first]);
  assert.deepEqual(uniqueCommits([first,second]),[first]);
});

test('conflicting mailmaps for one commit produce one canonical contributor with every repository association', () => {
  const first=makeCommit({repo:'example/a',email:'canonical@example.test',added:3,removed:1});
  const second=makeCommit({repo:'example/z',email:'other@example.test',added:300,removed:100});
  const contributors=people([second,first]);
  assert.equal(contributors.length,1);
  assert.equal(contributors[0].email,'canonical@example.test');
  assert.equal(contributors[0].commits,1);
  assert.equal(contributors[0].added,3);
  assert.deepEqual([...contributors[0].repos].sort(),['example/a','example/z']);
});

test('records without object IDs remain distinct even when all visible fields match', () => {
  const record=makeCommit({id:''});
  assert.equal(uniqueCommits([record,{...record}]).length,2);
  assert.equal(totals([record,{...record}]).commits,2);
});

test('since-last-look applies an exact exclusive timestamp boundary independently of range presets', () => {
  const since='2026-10-27T12:00:00Z';
  const snapshot={commits:[
    makeCommit({id:'before',date:'2026-10-27T11:59:59Z'}),
    makeCommit({id:'equal',date:since}),
    makeCommit({id:'after',date:'2026-10-27T12:00:01Z'}),
  ],pullRequests:[
    makePR({number:1,updatedAt:since}),
    makePR({number:2,updatedAt:'2026-10-27T12:00:01Z'}),
  ]};
  assert.deepEqual(selectCommits(snapshot,{days:0,since},now).map(c=>c.id),['after']);
  assert.deepEqual(filterPRs(snapshot,{days:0,since},now).map(p=>p.number),[2]);
});

test('PR facets match any selected value within a category and every selected category', () => {
  const snapshot={viewer:'alex',pullRequests:[
    makePR({number:1,author:'alex',review:'APPROVED',checks:'SUCCESS'}),
    makePR({number:2,author:'sam',review:'CHANGES_REQUESTED',checks:'ERROR'}),
    makePR({number:3,author:'sam',review:'REVIEW_REQUIRED',checks:'PENDING'}),
    makePR({number:4,author:'jules',review:'APPROVED',checks:'SUCCESS'}),
  ]};
  assert.deepEqual(filterPRs(snapshot,{author:['alex','sam'],review:['APPROVED','CHANGES_REQUESTED'],checks:['SUCCESS','ERROR']},now).map(p=>p.number).sort(),[1,2]);
});

test('needs-my-attention matches the complete TUI triage contract', () => {
  const oldReview={author:'ALEX',submittedAt:'2026-10-26T12:00:00Z'};
  const snapshot={viewer:'alex',pullRequests:[
    makePR({number:1,author:'ALEX',checks:'FAILURE'}),
    makePR({number:2,author:'alex',review:'CHANGES_REQUESTED'}),
    makePR({number:3,author:'alex',review:'APPROVED',checks:'SUCCESS'}),
    makePR({number:4,author:'alex',review:'APPROVED',checks:'SUCCESS',draft:true}),
    makePR({number:5,author:'alex',checks:'PENDING'}),
    makePR({number:6,author:'sam',reviewers:['ALEX']}),
    makePR({number:7,author:'sam',reviews:[oldReview]}),
    makePR({number:8,author:'sam',reviewers:['sample/alex']}),
    makePR({number:9,author:'sam',checks:'FAILURE'}),
    makePR({number:10,author:'alex',checks:'ERROR'}),
    makePR({number:11,author:'alex',reviews:[oldReview],checks:'PENDING'}),
    makePR({number:12,author:'alex',reviewers:['alex'],checks:'PENDING'}),
  ]};
  assert.deepEqual(filterPRs(snapshot,{mine:'attention'},now).map(p=>p.number).sort((a,b)=>a-b),[1,2,3,6,7,10]);
});

test('updated-since-review compares against the latest known submitted review and excludes missing dates', () => {
  const snapshot={viewer:'alex',pullRequests:[
    makePR({number:1,reviews:[{author:'alex',submittedAt:'2026-10-26T12:00:00Z'}]}),
    makePR({number:2,reviews:[{author:'alex',submittedAt:'2026-10-26T12:00:00Z'},{author:'ALEX',submittedAt:'2026-10-28T12:00:00Z'}]}),
    makePR({number:3,reviews:[{author:'alex',submittedAt:'0001-01-01T00:00:00Z'}]}),
    makePR({number:4,reviews:[{author:'sam',submittedAt:'2026-10-26T12:00:00Z'}]}),
    makePR({number:5,reviews:[{author:'alex',submittedAt:'2026-10-27T12:00:00Z'}]}),
  ]};
  assert.deepEqual(filterPRs(snapshot,{mine:'updated'},now).map(p=>p.number),[1]);
});

test('PR author sorting is case insensitive with newest updates first and supports whole-order reversal', () => {
  const snapshot={pullRequests:[
    makePR({number:1,author:'zora',title:'Bravo'}),
    makePR({number:2,author:'alex',title:'Zulu',updatedAt:'2026-10-26T12:00:00Z'}),
    makePR({number:3,author:'Alex',title:'alpha',updatedAt:'2026-10-28T12:00:00Z'}),
  ]};
  assert.deepEqual(filterPRs(snapshot,{sort:'author'},now).map(p=>p.number),[3,2,1]);
  assert.deepEqual(filterPRs(snapshot,{sort:'author',reverse:true},now).map(p=>p.number),[1,2,3]);
  assert.deepEqual(filterPRs(snapshot,{sort:'title'},now).map(p=>p.number),[3,1,2]);
});


test('date presets include the existing one-day and fourteen-day windows', () => {
  assert.deepEqual(ranges.map(r=>r.value),[1,7,14,30,90,365,0]);
});

test('Activity contributor filtering keeps canonical shared-commit repository evidence', () => {
  const canonical=makeCommit({repo:'example/a',email:'canonical@example.test'});
  const copy=makeCommit({repo:'example/z',email:'different@example.test'});
  const other=makeCommit({id:'other',email:'other@example.test'});
  const selected=selectCommits({commits:[copy,canonical,other]},{people:[identity(canonical)]},now);
  assert.deepEqual(selected.map(c=>c.repo).sort(),['example/a','example/z']);
  assert.equal(totals(selected).commits,1);
});

test('timeline switches daily, Sunday-aligned weekly, and monthly buckets without losing selected counts', () => {
  const localNoon=new Date(2026,9,28,12);
  const records=[makeCommit({date:localNoon.toISOString()})];
  const daily=timelineSeries(records,14,localNoon.getTime());
  const weekly=timelineSeries(records,90,localNoon.getTime());
  const monthly=timelineSeries(records,365,localNoon.getTime());
  assert.ok(daily.length>=14&&daily.length<=15);
  assert.ok(weekly.length>=13&&weekly.length<=14);
  assert.ok(weekly.every(row=>new Date(row.date).getDay()===0));
  assert.equal(monthly.length,12);
  assert.ok(monthly.every(row=>new Date(row.date).getDate()===1));
  for(const rows of [daily,weekly,monthly])assert.equal(rows.reduce((sum,row)=>sum+row.commits,0),1);
});

test('related evidence distinguishes exact shared commit IDs from contributor overlap', () => {
  const source=makeCommit({repo:'example/a'});
  const shared=makeCommit({repo:'example/b'});
  const overlap=makeCommit({id:'unrelated',repo:'example/c'});
  const anonymous=makeCommit({id:'',repo:'example/d',email:'someone@example.test'});
  const records=[source,shared,overlap,anonymous];
  assert.deepEqual(relatedScopes(records,{repo:'example/a',path:''},'commits').map(r=>r.repo),['example/b']);
  assert.deepEqual(relatedScopes(records,{repo:'example/a',path:''},'people').map(r=>r.repo),['example/b','example/c']);
});

test('related contributor overlap uses canonical identities while retaining repository copies', () => {
  const canonical=makeCommit({repo:'example/a',author:'Canonical',email:'canonical@example.test'});
  const alias={...canonical,repo:'example/z',author:'Alias',email:'alias@example.test'};
  const overlap={...canonical,id:'other',repo:'example/b'};
  const aliasOnly={...alias,id:'alias-only',repo:'example/c'};
  const records=[alias,canonical,overlap,aliasOnly];
  for(const ordered of [records,[...records].reverse()]){
    const related=relatedScopes(ordered,{repo:'example/z',path:''},'people');
    assert.deepEqual(related.map(r=>r.repo),['example/a','example/b']);
    assert.ok(related.every(r=>r.count===1&&r.evidence[0]===identity(canonical)));
    assert.deepEqual(related.find(r=>r.repo==='example/a').records,[canonical]);
    const reverse=relatedScopes(ordered,{repo:'example/a',path:''},'people');
    assert.deepEqual(reverse.find(r=>r.repo==='example/z').records,[alias]);
  }
});

test('related contributor drill retains scoped counts with canonical author attribution', () => {
  const canonical=makeCommit({repo:'example/a',author:'Canonical',email:'canonical@example.test',added:999,removed:999});
  const alias=makeCommit({repo:'example/z',author:'Alias',email:'alias@example.test'});
  const records=[alias,canonical];
  const scoped=scopedCommits(records,{repo:'example/z',path:'src'});
  const selected=filterByCanonicalIdentity(scoped,[identity(canonical)],records);
  assert.deepEqual(selected,scoped);
  assert.equal(filterByCanonicalIdentity(scoped,[identity(alias)],records).length,0);
  const [person]=people(selected,records);
  assert.equal(person.id,identity(canonical));
  assert.equal(person.name,'Canonical');
  assert.equal(person.commits,1);
  assert.equal(person.added,80);
  assert.equal(person.removed,20);
  assert.deepEqual(person.repos,['example/z']);
  assert.deepEqual(person.records[0].files.map(f=>f.path),['src/queue.go']);
  assert.equal(alias.author,'Alias');
});

test('named work areas choose the longest literal prefix and retain only scoped file volume', () => {
  const rules=[{name:'Backend',paths:['src/']},{name:'Auth',paths:['src/auth']},{name:'Token',paths:['src/auth/token.go']},{name:'Literal',paths:['literal*']}];
  assert.equal(classifyWorkArea('src/auth/token.go',rules),'Token');
  assert.equal(classifyWorkArea('src/auth/login.go',rules),'Auth');
  assert.equal(classifyWorkArea('src/authentication/check.go',rules),'Backend');
  assert.equal(classifyWorkArea('literal*/file',rules),'Literal');
  assert.equal(classifyWorkArea('literal-other/file',rules),'');
  const records=[makeCommit({files:[{path:'src/auth/login.go',added:4,removed:2},{path:'src/auth/token.go',added:8,removed:1},{path:'docs/auth.md',added:3,removed:0}]})];
  const scoped=scopedCommits(records,{repo:'sample/api',areaName:'Auth',areaRules:rules});
  assert.equal(totals(scoped).added,4);
  assert.equal(totals(scoped).removed,2);
  assert.deepEqual(relatedScopes(records,{repo:'sample/api',areaName:'Auth',areaRules:rules},'commits',{'sample/api':rules}).filter(r=>r.areaName).map(r=>r.areaName),['Token']);
});

test('PR author facets compare GitHub logins case insensitively', () => {
  const snapshot={pullRequests:[makePR({author:'Alex'})]};
  assert.equal(filterPRs(snapshot,{author:['alex']},now).length,1);
});

test('range-aware timelines retain future-dated selected commits consistently with totals', () => {
  const current=new Date(2026,9,28,12);
  const future=new Date(2026,10,5,12);
  const records=[makeCommit({date:future.toISOString()})];
  for(const days of [14,90,365]){
    const selected=selectCommits({commits:records},{days},current.getTime());
    assert.equal(totals(selected).commits,1);
    assert.equal(timelineSeries(selected,days,current.getTime()).reduce((sum,row)=>sum+row.commits,0),1);
  }
});

test('excluded lines are summed by reason once per shared commit', () => {
  const shared = makeCommit({id:'shared', excludedBulkLines:2500, excludedPathLines:40});
  const result = excludedLines([shared, {...shared, repo:'sample/web'}, makeCommit({id:'other', excludedAttributeLines:7}), makeCommit({id:'plain'})]);
  assert.deepEqual(result, {path:40, attribute:7, bulk:2500, total:2547});
  assert.deepEqual(excludedLines([]), {path:0, attribute:0, bulk:0, total:0});
});

test('heat compares each week with the twelve weeks before it', () => {
  const at = weeksAgo => new Date(now - weeksAgo * 7 * 86400000).toISOString();
  const records = [];
  for (let w = 37; w >= 4; w--) records.push(makeCommit({id:`steady-${w}`, date:at(w), added:10, removed:0}));
  for (let i = 0; i < 4; i++) for (let w = 0; w < 4; w++) records.push(makeCommit({id:`burst-${w}-${i}`, date:at(w), added:10, removed:0}));
  const heat = heatSeries(records, 'lines', now);
  assert.equal(heat.series.length, HEAT.weeks);
  assert.equal(heat.series.at(-5).value, 10);
  assert.equal(heat.series.at(-5).usual, 10);
  assert.equal(heat.series.at(-5).level, 'steady');
  assert.equal(heat.series.at(-4).value, 40);
  assert.equal(heat.series.at(-4).level, 'hot', 'the first burst week is four times its usual pace');
  assert.equal(heat.series.at(-1).usual, 17.5, 'later burst weeks include earlier bursts in their baseline');
  assert.equal(heat.series.at(-1).level, 'warm');
  assert.equal(heat.prior, 10);
  assert.equal(heat.recent, 40);
  assert.equal(heat.ratio, 4);
  assert.equal(heat.status, 'up');
  assert.equal(heatSeries(records, 'commits', now).series.at(-1).value, 4);
  assert.equal(heatSeries(records, 'people', now).series.at(-1).value, 1);
});

test('heat levels and statuses keep empty, new and quiet evidence distinct', () => {
  assert.equal(heatLevel(0, 10), 'none');
  assert.equal(heatLevel(5, 0), 'first');
  assert.equal(heatLevel(30, 10), 'hot');
  assert.equal(heatLevel(18, 10), 'warm');
  assert.equal(heatLevel(10, 10), 'steady');
  assert.equal(heatLevel(5, 10), 'cool');
  assert.equal(heatLevel(2, 10), 'cold');
  const at = weeksAgo => new Date(now - weeksAgo * 7 * 86400000).toISOString();
  assert.equal(heatSeries([makeCommit({date:at(1)})], 'lines', now).status, 'new');
  assert.equal(heatSeries([makeCommit({date:at(8)})], 'lines', now).status, 'quiet');
  const unknown = heatSeries([makeCommit({date:at(1), linesUnknown:true})], 'lines', now);
  assert.equal(unknown.series.at(-2).value, 0, 'unknown line counts do not become measured lines');
  assert.equal(heatSeries([makeCommit({date:at(1), linesUnknown:true})], 'commits', now).series.at(-2).value, 1);
  const future = heatSeries([makeCommit({date:'2027-01-01T00:00:00Z'})], 'lines', now);
  assert.equal(future.future, 1);
  assert.equal(future.total, 0);
});

test('heat rows follow the Activity scope and keep quiet repositories visible', () => {
  const at = weeksAgo => new Date(now - weeksAgo * 7 * 86400000).toISOString();
  const commits = [
    makeCommit({id:'a', date:at(1), files:[{path:'src/api/queue.go',added:80,removed:20},{path:'docs/queue.md',added:40,removed:10}]}),
    makeCommit({id:'b', date:at(60), files:[{path:'legacy/old.go',added:5,removed:5}], added:5, removed:5}),
  ];
  const repositories = [{id:'sample/api',name:'api'},{id:'sample/quiet',name:'quiet'}];
  const org = heatRows(commits, repositories, {}, 'lines', now);
  assert.deepEqual(org.rows.map(r => r.id), ['sample/api','sample/quiet']);
  assert.equal(org.rows[1].total, 0);
  const repo = heatRows(commits, repositories, {repo:'sample/api', path:''}, 'lines', now);
  assert.deepEqual(repo.rows.map(r => [r.id, r.total]), [['src',100],['docs',50]], 'directories outside the window are omitted');
  assert.equal(repo.scope.total, 150);
  assert.equal(heatRows(commits, repositories, {repo:'sample/api', path:'src'}, 'lines', now).rows[0].id, 'src/api');
});
