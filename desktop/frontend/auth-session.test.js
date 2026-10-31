import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const source=readFileSync(new URL('./app.js',import.meta.url),'utf8');
const functions=source.slice(source.indexOf('async function openAuthenticatedWorkspace(){'),source.indexOf('async function discoverSetupRepositories(){'));
function harness(){
 const scheduled=[];
 const sandbox={state:{snapshot:{auth:{signedIn:false}}},sessionVersion:0,authTimer:null,
 scheduled,setTimeout:fn=>{scheduled.push(fn);return 1;},clearTimeout:()=>{},
 validSnapshot:value=>value,applyPreferences:()=>{},render:()=>{},renderModal:()=>{},openModal:()=>{},refresh:async()=>{}};
 const ctx=vm.createContext(sandbox);
 vm.runInContext('function resetSessionView(){sessionVersion++;}'+functions,ctx);
 return ctx;
}

test('late authenticated workspace response cannot undo sign-out',async()=>{
 const ctx=harness();let resolve;
 ctx.api=()=>new Promise(r=>{resolve=r;});
 const loading=vm.runInContext('openAuthenticatedWorkspace()',ctx);
 ctx.sessionVersion++;ctx.state.snapshot={auth:{signedIn:false}};
 resolve({auth:{signedIn:true},status:{phase:'ready'},commits:[{subject:'private evidence'}]});
 await loading;
 assert.equal(ctx.state.snapshot.auth.signedIn,false);
 assert.equal(ctx.state.snapshot.commits,undefined);
});

test('late authentication polling cannot undo cancel or sign-out',async()=>{
 const ctx=harness();let resolve;
 ctx.api=()=>new Promise(r=>{resolve=r;});
 vm.runInContext('pollSignIn()',ctx);
 const polling=ctx.scheduled.shift()();
 ctx.sessionVersion++;ctx.state.snapshot={auth:{signedIn:false}};
 resolve({signedIn:true,pending:false});
 await polling;
 assert.equal(ctx.state.snapshot.auth.signedIn,false);
 assert.equal(ctx.scheduled.length,0);
});
