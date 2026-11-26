#!/usr/bin/env node
// Real headless Chrome/CDP check: loaded images, layout, anchor click, screenshots.
import {spawn} from 'node:child_process';
import {mkdtemp, mkdir, writeFile, readFile, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
const base=process.argv[2]||'http://127.0.0.1:18082';
const output=process.argv[3]||'docs/screenshots';
const profile=await mkdtemp(path.join(tmpdir(),'apk-browser-'));
const chrome=spawn('/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',['--headless=new','--disable-gpu','--no-first-run','--no-default-browser-check','--remote-debugging-pipe',`--user-data-dir=${profile}`],{stdio:['ignore','ignore','ignore','pipe','pipe']});
let next=0,buffer='';const pending=new Map();
chrome.stdio[4].on('data',data=>{buffer+=data;let end;while((end=buffer.indexOf('\0'))>=0){const msg=JSON.parse(buffer.slice(0,end));buffer=buffer.slice(end+1);if(msg.id){const task=pending.get(msg.id);if(task){pending.delete(msg.id);clearTimeout(task.timer);msg.error?task.reject(Error(JSON.stringify(msg.error))):task.resolve(msg.result);}}}});
function send(method,params={},sessionId){return new Promise((resolve,reject)=>{const id=++next;const timer=setTimeout(()=>{pending.delete(id);reject(Error('CDP timeout '+method))},30000);pending.set(id,{resolve,reject,timer});chrome.stdio[3].write(JSON.stringify({id,method,params,...(sessionId?{sessionId}:{})})+'\0');});}
function assert(value,message){if(!value)throw Error(message);}
const results=[];
try{
 await mkdir(output,{recursive:true});
 const {targetId}=await send('Target.createTarget',{url:'about:blank'});
 const {sessionId}=await send('Target.attachToTarget',{targetId,flatten:true});
 const call=(method,params)=>send(method,params,sessionId);
 const evaluate=async expression=>{const r=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true,timeout:15000});if(r.exceptionDetails)throw Error(JSON.stringify(r.exceptionDetails));return r.result.value;};
 await call('Page.enable');
 for(const [name,width,height,mobile] of [['desktop',1440,1000,false],['mobile',390,844,true]]){
  await call('Emulation.setDeviceMetricsOverride',{width,height,deviceScaleFactor:1,mobile});
  await call('Page.navigate',{url:base+'/apps/sample-app-one?lan=id'});
  for(let i=0;i<100;i++){
   if(await evaluate(`document.readyState==='complete' && location.pathname==='/apps/sample-app-one'`))break;
   await new Promise(r=>setTimeout(r,100));
  }
  await evaluate(`document.querySelectorAll('img').forEach(i=>i.loading='eager')`);
  await evaluate(`Promise.race([Promise.all([...document.images].map(i=>i.decode().catch(()=>{}))),new Promise(r=>setTimeout(r,10000))])`);
  await evaluate(`document.fonts.ready`);
  const info=await evaluate(`({title:document.title,language:document.documentElement.lang,width:innerWidth,scrollWidth:document.documentElement.scrollWidth,height:document.documentElement.scrollHeight,images:[...document.images].map(i=>({src:i.getAttribute('src'),loaded:i.complete&&i.naturalWidth>0})),links:document.querySelectorAll('a[href]').length,cta:document.querySelector('a.primary-action')?.getAttribute('href')})`);
  assert(info.language==='id','not Indonesian');assert(info.title.length>5,'empty page');assert(info.scrollWidth<=width,'horizontal overflow');assert(info.images.length>0&&info.images.every(i=>i.loaded),'broken images');assert(info.cta==='#apk-info','CTA changed');
  const shot=await call('Page.captureScreenshot',{format:'png',captureBeyondViewport:true,clip:{x:0,y:0,width,height:info.height,scale:1}});
  const file=path.join(output,`go-parity-${name}.png`);await writeFile(file,Buffer.from(shot.data,'base64'));
  await evaluate(`document.querySelector('a.primary-action').click()`);
  const anchor=await evaluate(`({hash:location.hash,exists:!!document.getElementById('apk-info')})`);
  assert(anchor.hash==='#apk-info'&&anchor.exists,'anchor click failed');
  results.push({name,...info,screenshot:file,anchor});console.log('PASS',name,info.links,'links',info.images.length,'images');
 }
 let admin;
 if(process.argv[4]){
  const env=Object.fromEntries((await readFile(process.argv[4],'utf8')).split('\n').filter(line=>line.includes('=')&&!line.startsWith('#')).map(line=>{const i=line.indexOf('=');return [line.slice(0,i),line.slice(i+1)]}));
  await call('Emulation.setDeviceMetricsOverride',{width:1440,height:1000,deviceScaleFactor:1,mobile:false});
  await call('Page.navigate',{url:base+'/admin/'});
  for(let i=0;i<100;i++){if(await evaluate(`!!document.querySelector('#loginForm') && typeof csrf!=='undefined' && csrf.length>0`))break;await new Promise(r=>setTimeout(r,100))}
  await evaluate(`document.querySelector('[name=username]').value=${JSON.stringify(env.ADMIN_USERNAME)};document.querySelector('[name=password]').value=${JSON.stringify(env.ADMIN_PASSWORD)};document.querySelector('#loginForm').requestSubmit()`);
  for(let i=0;i<100;i++){if(await evaluate(`document.querySelector('#workspace')?.hidden===false && document.querySelector('#list').textContent.length>10`))break;await new Promise(r=>setTimeout(r,100))}
  admin=await evaluate(`({loggedIn:document.querySelector('#workspace')?.hidden===false,message:document.querySelector('#message')?.textContent,passwordCleared:document.querySelector('[name=password]').value==='',cookieHidden:!document.cookie.includes('apk_admin_session'),tabs:[...document.querySelectorAll('#tabs button')].map(b=>b.textContent),overflow:document.documentElement.scrollWidth>innerWidth})`);
  assert(admin.loggedIn&&admin.passwordCleared&&admin.cookieHidden&&!admin.overflow,'admin browser login/cookie/layout failed');
  const shot=await call('Page.captureScreenshot',{format:'png',captureBeyondViewport:false});await writeFile(path.join(output,'go-admin-desktop.png'),Buffer.from(shot.data,'base64'));
  admin.mobile=[];
  for(const key of ['settings','external','main','domains','internal']){
   await evaluate(`selected=${JSON.stringify(key)};safely(load)`);
   for(let i=0;i<100;i++){if(await evaluate(`loadedCollection===${JSON.stringify(key)} && !document.querySelector('#save').disabled`))break;await new Promise(r=>setTimeout(r,100))}
   assert(await evaluate(`loadedCollection===${JSON.stringify(key)}`),'admin tab did not load: '+key);
   for(const width of [390,320]){
    await call('Emulation.setDeviceMetricsOverride',{width,height:844,deviceScaleFactor:1,mobile:true});
    await evaluate(`scrollTo(0,0)`);
    const info=await evaluate(`({width:innerWidth,scrollWidth:document.documentElement.scrollWidth,heading:document.querySelector('h1').getBoundingClientRect().width,importVisible:!document.querySelector('#importSection').hidden,formatHelpVisible:!document.querySelector('#importFormatHelp').hidden})`);
    admin.mobile.push({tab:key,...info});assert(info.scrollWidth<=width,'admin mobile overflow '+key+' at '+width);
    assert(info.importVisible===(key!=='settings'),'import panel visibility '+key);
    assert(info.formatHelpVisible===(key==='external'||key==='main'),'format guidance visibility '+key);
    const mobileShot=await call('Page.captureScreenshot',{format:'png',captureBeyondViewport:false});await writeFile(path.join(output,`go-admin-${key}-${width}.png`),Buffer.from(mobileShot.data,'base64'));
    if(info.importVisible){
     await evaluate(`document.querySelector('#importSection').scrollIntoView()`);
     const importShot=await call('Page.captureScreenshot',{format:'png',captureBeyondViewport:false});await writeFile(path.join(output,`go-admin-${key}-import-${width}.png`),Buffer.from(importShot.data,'base64'));
    }
   }
  }

  await evaluate(`document.querySelector('#logout').click()`);
  for(let i=0;i<100;i++){if(await evaluate(`document.querySelector('#login').hidden===false`))break;await new Promise(r=>setTimeout(r,100))}
  admin.loggedOut=await evaluate(`document.querySelector('#login').hidden===false`);assert(admin.loggedOut,'browser logout failed');console.log('PASS admin login/logout, cookie isolation and layout');
 }
 await writeFile(path.join(output,'browser-acceptance.json'),JSON.stringify({base,results,admin},null,2));
}catch(error){console.error(error);process.exitCode=1;}finally{
 try{await send('Browser.close')}catch{}
 chrome.kill();
 await new Promise(resolve=>{if(chrome.exitCode!==null)return resolve();chrome.once('exit',resolve);setTimeout(resolve,3000)});
 await rm(profile,{recursive:true,force:true,maxRetries:5,retryDelay:300}).catch(error=>console.warn('Profile cleanup deferred:',error.code));
}
