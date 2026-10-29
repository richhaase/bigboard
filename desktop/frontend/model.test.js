import test from 'node:test';
import assert from 'node:assert/strict';
import {
  activityRows, dailySeries, dateValue, filterPRs, inRange, people, safeURL,
  scopedCommits, selectCommits, sortRows, totals, uniqueCommits,
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
  assert.equal(inRange('2026-10-21T12:00:00Z',7,now),true);
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
    makeCommit({id:'bot',bot:true}),
  ]};
  const selected = selectCommits(snapshot,{repos:['sample/web'],days:7,hideBots:true},now);
  assert.equal(selected.length,1);
  assert.equal(selected[0].repo,'sample/web');
  assert.equal(totals(selectCommits(snapshot,{days:7,hideBots:true},now)).commits,1);
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
