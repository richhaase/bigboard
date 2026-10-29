import assert from 'node:assert/strict';
import {execFileSync,spawn} from 'node:child_process';
import {mkdtemp,mkdir,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const {chromium}=await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const root=path.dirname(fileURLToPath(import.meta.url));
const temporary=await mkdtemp(path.join(tmpdir(),'bigboard-gui-test-'));
const screenshots=process.env.BIGBOARD_SCREENSHOTS || path.join(root,'build/screenshots');
await mkdir(screenshots,{recursive:true});
const binary=path.join(temporary,'bigboard');
execFileSync('go',['build','-o',binary,'.'],{cwd:root,stdio:'inherit'});
const server=spawn(binary,['--serve','--addr','127.0.0.1:0'],{env:{...process.env,XDG_CONFIG_HOME:path.join(temporary,'config'),XDG_CACHE_HOME:path.join(temporary,'cache')},stdio:['ignore','pipe','pipe']});
const address=await new Promise((resolve,reject)=>{
 const timer=setTimeout(()=>reject(new Error('Preview failed to start')),15000);
 server.stdout.on('data',chunk=>{const match=chunk.toString().match(/http:\/\/127\.0\.0\.1:\d+/);if(match){clearTimeout(timer);resolve(match[0]);}});
 server.once('error',reject);
 server.once('exit',code=>{clearTimeout(timer);reject(new Error(`Preview exited ${code}`));});
 server.stderr.on('data',chunk=>process.stderr.write(chunk));
});
let browser;
let releaseRefresh;
try {
 browser=await chromium.launch({headless:true});
 const page=await browser.newPage({viewport:{width:1360,height:900},reducedMotion:'reduce'});
 const errors=[];
 page.on('pageerror',error=>errors.push(error.message));
 const click=async selector=>page.locator(selector).click();
 const now=new Date().toISOString();
 const settings={mode:'live',personalAccount:'',organizations:['test-org'],repositories:[]};
 const repositories=['test-org/alpha','test-org/beta'].map(id=>({id,name:id.split('/')[1],branch:'main',revision:'a'.repeat(40),stale:false,error:''}));
 const commits=Array.from({length:210},(_,i)=>({id:i.toString(16).padStart(40,'0'),repo:repositories[i<205?0:1].id,author:`Contributor ${i%3+1}`,email:`contributor${i%3+1}@example.test`,date:now,subject:`Change ${i+1}`,added:10,removed:3,ai:i%3===0,bot:false,linesUnknown:false,files:[{path:'src/app.go',added:10,removed:3}]}));
 const pullRequests=[{repo:repositories[0].id,number:12,title:'Improve loading feedback',author:'contributor1',review:'REVIEW_REQUIRED',checks:'FAILURE',files:['src/app.go']},{repo:repositories[0].id,number:13,title:'Refine settings',author:'contributor2',review:'APPROVED',checks:'SUCCESS',files:['settings.go']},{repo:repositories[1].id,number:14,title:'Refresh history',author:'contributor3',review:'REVIEW_REQUIRED',checks:'PENDING',files:['history.go']}].map(p=>({...p,url:`https://github.com/${p.repo}/pull/${p.number}`,updatedAt:now,draft:false,complete:true,reviewers:['contributor1']}));
 const loaded={mode:'live',generatedAt:now,viewer:'contributor1',settings,repositories,commits,pullRequests,status:{phase:'ready',message:'Workspace ready',errors:[],cachePath:'/test/cache'}};
 let snapshot={...loaded,generatedAt:'',repositories:[],commits:[],pullRequests:[],status:{...loaded.status,phase:'initial',message:'Loading repository history…'}};
 let failRefresh=false;
 let refreshCount=0;
 let snapshotReads=0;
 await page.route('**/api/**',async route=>{
  const endpoint=new URL(route.request().url()).pathname;
  const send=data=>route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(data)});
  if(endpoint==='/api/snapshot'){snapshotReads++;return send(snapshot);}
  if(endpoint==='/api/refresh'){
   refreshCount++;
   if(failRefresh)return route.fulfill({status:500,contentType:'application/json',body:JSON.stringify({error:'Test refresh failure'})});
   if(refreshCount===1){
    snapshot={...loaded,pullRequests:[],status:{...loaded.status,phase:'refreshing',message:'Cached history loaded; refreshing GitHub…'}};
    await new Promise(resolve=>{releaseRefresh=resolve;});
   }
   snapshot={...loaded,settings:snapshot.settings,mode:snapshot.settings.mode,pullRequests:snapshot.settings.mode==='cache'?[]:pullRequests};
   return send(snapshot);
  }
  if(endpoint==='/api/discover')return send(repositories);
  if(endpoint==='/api/settings'){
   const updated=route.request().postDataJSON();
   snapshot={...loaded,settings:updated,mode:updated.mode,pullRequests:updated.mode==='cache'?[]:pullRequests};
   return send(snapshot);
  }
  return route.abort();
 });
 await page.goto(address);
 await page.getByText('Loading your repositories',{exact:true}).waitFor();
 await page.locator('[data-row="activity"]').first().waitFor();
 assert.equal(await page.locator('[data-row="activity"]').count(),2);
 assert.equal(await page.locator('[data-action="refresh"]').isDisabled(),true,'cached history appears before remote refresh completes');
 await page.getByText('Cached history loaded; refreshing GitHub…',{exact:true}).waitFor();
 await page.getByText('Loading pull requests from GitHub…',{exact:true}).waitFor();
 await click('[data-action="board"][data-value="prs"]');
 await page.getByRole('heading',{name:'Loading pull requests',exact:true}).waitFor();
 assert.equal(await page.locator('.pr-context > div').first().locator('strong').innerText(),'—');
 await click('[data-action="board"][data-value="activity"]');
 releaseRefresh();
 await page.waitForFunction(()=>!document.querySelector('[data-action="refresh"]').disabled);
 const settledReads=snapshotReads;
 await page.waitForTimeout(1600);
 assert.equal(snapshotReads,settledReads,'snapshot polling stops after refresh completion');
 await page.context().grantPermissions(['clipboard-read','clipboard-write']);
 await click('[data-action="copy-view"]');
 assert.ok((await page.evaluate(()=>navigator.clipboard.readText())).includes('test-org/alpha'));
 await page.screenshot({path:path.join(screenshots,'activity.png'),fullPage:true});
 await page.getByLabel('Search activity').fill('no-such-repository');
 await page.getByText('No activity matches',{exact:true}).waitFor();
 await page.getByLabel('Search activity').fill('');
 await page.locator('[data-action="explore"]').first().click();
 let depth=0;
 while(await page.locator('[data-action="explore"]').count()&&depth++<8){await page.locator('[data-action="explore"]').first().click();}
 assert.equal(await page.locator('.commit').count(),100,'initial file history is bounded');
 await click('[data-action="more-commits"]');
 assert.equal(await page.locator('.commit').count(),200,'remaining history can be loaded');
 await click('[data-action="board"][data-value="people"]');
 assert.equal(await page.locator('.contributor-row').count(),3);
 await page.locator('.contributor-row').nth(1).click();
 assert.ok(await page.locator('.detail-person h2').innerText());
 assert.ok(await page.locator('.heat-cell').count()>0);
 await page.screenshot({path:path.join(screenshots,'contributors.png'),fullPage:true});
 await click('[data-action="board"][data-value="prs"]');
 assert.equal(await page.locator('.pr-row').count(),3);
 await page.getByLabel('Any checks',{exact:true}).selectOption('FAILURE');
 assert.equal(await page.locator('.pr-row').count(),1);
 assert.ok((await page.locator('.pr-row').allTextContents()).every(text=>text.includes('Failing')));
 await click('[data-action="reset-pr"]');
 await page.screenshot({path:path.join(screenshots,'pull-requests.png'),fullPage:true});
 await click('[data-action="repositories"]');
 const selectedRepo=await page.locator('[data-scope-repo]').first().getAttribute('data-scope-repo');
 await page.locator('[data-scope-repo]').first().check();
 await click('[data-action="apply-scope"]');
 assert.ok((await page.locator('.pr-row').allTextContents()).every(text=>text.includes(selectedRepo)));
 await click('[data-action="board"][data-value="activity"]');
 await click('[data-action="activity-root"]');
 assert.equal(await page.locator('[data-row="activity"]').count(),1);
 await page.keyboard.press('/');
 assert.equal(await page.getByLabel('Search activity').evaluate(el=>el===document.activeElement),true);
 await page.getByLabel('Search activity').blur();
 await page.keyboard.press('F1');
 await page.getByRole('dialog').waitFor();
 await page.keyboard.press('Escape');
 await click('.sidebar [data-action="settings"].nav-item');
 assert.deepEqual(await page.locator('[data-action="settings-mode"]').evaluateAll(items=>items.map(x=>x.dataset.value)),['live','cache']);
 await click('[data-action="settings-mode"][data-value="cache"]');
 await click('[data-action="save-settings"]');
 await page.getByText('Cached history',{exact:true}).waitFor();
 await page.reload();
 await page.getByText('Cached history',{exact:true}).waitFor();
 await click('[data-action="board"][data-value="prs"]');
 await page.getByText('PR context is unavailable offline',{exact:true}).waitFor();
 await click('.sidebar [data-action="settings"].nav-item');
 await click('[data-action="settings-mode"][data-value="live"]');
 await click('[data-action="save-settings"]');
 await click('[data-action="board"][data-value="activity"]');
 await page.locator('[data-row="activity"]').first().waitFor();
 failRefresh=true;
 await click('[data-action="refresh"]');
 await page.getByRole('alert').waitFor();
 assert.equal(await page.locator('[data-row="activity"]').count(),2,'failed refresh retains rendered snapshot');
 failRefresh=false;
 await click('[data-action="dismiss-error"]');
 for(const width of [860,390]){
  await page.setViewportSize({width,height:900});
  assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),`no page overflow at ${width}px`);
  await page.screenshot({path:path.join(screenshots,`activity-${width}.png`),fullPage:true});
 }
 snapshot={...loaded,pullRequests:[]};
 await page.reload();
 await click('[data-action="board"][data-value="prs"]');
 await page.getByText('No pull requests match',{exact:true}).waitFor();
 assert.equal(await page.locator('.pr-context > div').first().locator('strong').innerText(),'0','complete empty PR evidence remains zero');
 snapshot={...loaded,mode:'cache',generatedAt:'',settings:{mode:'cache',personalAccount:'',organizations:[],repositories:[]},repositories:[],commits:[],pullRequests:[],status:{...loaded.status,phase:'setup',message:'Choose an account or organization to load your repositories.'}};
 await page.unroute('**/api/**');
 await page.route('**/api/**',route=>route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(snapshot)}));
 await page.reload();
 await page.getByRole('dialog').waitFor();
 await page.getByRole('heading',{name:'Workspace settings',exact:true}).waitFor();
 assert.deepEqual(errors,[],'no uncaught browser errors');
 console.log('Browser checks passed: real-data loading flow, interim cached history, bounded history rendering, three boards, filters, settings, empty setup, failure retention, responsive layout. Test fixture data exists only in intercepted API responses.');
 console.log(`Screenshots: ${screenshots}`);
} finally {
 releaseRefresh?.();
 await browser?.close();
 const exited=new Promise(resolve=>server.once('exit',resolve));
 server.kill('SIGTERM');
 await exited;
 await rm(temporary,{recursive:true,force:true});
}
