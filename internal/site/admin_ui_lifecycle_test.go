package site

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Keep the delivered JavaScript, rather than a parallel mock implementation,
// under regression coverage for responses that outlive a view or login session.
func TestAdminTemplateMutationAndAuthenticationLifecycles(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to execute the delivered admin JavaScript")
	}
	html, err := os.ReadFile("templates/admin.html")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.SplitN(strings.SplitN(string(html), "<script>", 2)[1], "</script>", 2)[0]
	cmd := exec.Command(node, "-e", adminUILifecycleJS)
	cmd.Stdin = bytes.NewBufferString(script)
	out, err := cmd.CombinedOutput()
	if err != nil || !bytes.Contains(out, []byte("PASS 16 lifecycle UI cases")) {
		t.Fatalf("actual admin JS lifecycle assertions: %v\n%s", err, out)
	}
}

const adminUILifecycleJS = `
const fs=require('fs'),vm=require('vm'),assert=require('node:assert/strict');
const source=fs.readFileSync(0,'utf8');
const response=(data,status=200)=>({ok:status<400,status,json:async()=>({data,message:'old request failed'})});
async function fixture(){
 const elements=new Map(),requests=[];
 function el(id){if(!elements.has(id))elements.set(id,{value:id==='siteKey'?'default':'',hidden:false,textContent:'',className:'',disabled:false,append(){},replaceChildren(){},add(){}});return elements.get(id)}
 const ctx=vm.createContext({document:{getElementById:el,createElement:()=>({})},setInterval(){},Option:function(){},confirm:()=>true,fetch:async(url,opt)=>{
  if(url.endsWith('/session'))return response({authenticated:false,csrf_token:'probe'});
  return new Promise(resolve=>requests.push({url,method:opt.method,body:opt.body&&JSON.parse(opt.body),resolve}));
 }});
 const run=s=>vm.runInContext(s,ctx);
 run(source);await new Promise(setImmediate);run('show(true)');
 async function load(key){run('selected='+JSON.stringify(key));const promise=run('load()'),request=requests.shift();assert.equal(request.method,'GET');request.resolve(response(key==='languages'?{bindings:[]}:{links:[{id:17,site_key:'default',domain:'source.test',anchor_text:'Source',target_url:'https://source.test/',lang:'id'}]}));await promise;}
 return {run,el,requests,load};
}
(async()=>{
 let cases=0;
 // A saved/deleted/imported row belongs to the starting view. Neither success
 // nor failure may reload a newer tab, erase its draft, or replace its message.
 for(const action of ['save','remove','import'])for(const status of [200,500]){
  const f=await fixture();await f.load('external');
  f.run("$('records').value='0';$('records').onchange();$('strategy').value='append';$('importText').value='https://source.test/,Source'");
  const pending=f.run("$('"+action+"').onclick()"),mutation=f.requests.shift();
  assert.ok(mutation.method!=='GET');
  for(const id of ['save','remove','import','editor','importText','strategy','date'])assert.equal(f.el(id).disabled,true,id+' pending');
  // Invoking the handler directly must not bypass pending-write protection.
  await f.run("$('"+action+"').onclick()");assert.equal(f.requests.length,0,'duplicate submission');
  await f.load('languages');f.el('editor').value='unsaved new language draft';f.el('importText').value='unrelated import draft';f.run("message('new view message')");
  mutation.resolve(response({deleted:1,imported:1},status));await pending;
  assert.equal(f.requests.length,0,'old mutation reloaded a newer view');
  assert.equal(f.el('editor').value,'unsaved new language draft');assert.equal(f.el('importText').value,'unrelated import draft');assert.equal(f.el('message').textContent,'new view message');assert.equal(f.el('save').disabled,false);
  cases++;
 }
 // A successful current-view mutation still reloads exactly its own collection.
 const f=await fixture();await f.load('external');
 const saving=f.run("$('save').onclick()"),write=f.requests.shift();
 write.resolve(response({saved:true}));await new Promise(setImmediate);
 assert.equal(f.requests.length,1);const reload=f.requests.shift();assert.equal(reload.url,'/admin/api/external-links?site_key=default');reload.resolve(response({links:[]}));await saving;assert.equal(f.el('save').disabled,false);cases++;
 // A committed save followed by a failed list refresh is not a write failure,
 // but the current error must be visible instead of a permanent Loading state.
 const h=await fixture();await h.load('external');
 const committed=h.run("$('save').onclick()"),mutation=h.requests.shift();mutation.resolve(response({saved:true}));await new Promise(setImmediate);
 const refresh=h.requests.shift();refresh.resolve(response({},500));await committed;
 assert.equal(h.el('message').textContent,'刷新记录失败：服务暂时不可用，请稍后重试。');assert.match(h.el('list').textContent,/加载记录失败，请点击“刷新列表”重试/);assert.equal(h.el('save').disabled,true);
 await h.load('external');assert.equal(h.el('save').disabled,false);cases++;
 // A failed older list load must not replace a newer view's message or editor.
 const k=await fixture();k.run("selected='external'");const obsolete=k.run('load()'),obsoleteRequest=k.requests.shift();await k.load('languages');k.run("message('new tab')");k.el('editor').value='new draft';obsoleteRequest.resolve(response({},500));await obsolete;
 assert.equal(k.el('message').textContent,'new tab');assert.equal(k.el('editor').value,'new draft');cases++;
 // Import mode is explicit and collection-scoped, never inferred from markup.
 for(const key of ['external','main','internal']){
  const f=await fixture();await f.load(key);assert.equal(f.el('importFormat').value,'legacy');
  assert.equal(f.el('importFormatLabel').hidden,key==='internal');assert.equal(f.el('importFormatHelp').hidden,key==='internal');f.el('importFormat').value='normalized';f.el('strategy').value='append';f.el('importText').value='https://source.test/,Source';
  const importing=f.run("$('import').onclick()"),request=f.requests.shift();
  assert.equal(request.body.format,key==='internal'?undefined:'normalized');assert.equal(f.el('importFormat').disabled,true);
  request.resolve(response({imported:1}));await new Promise(setImmediate);f.requests.shift().resolve(response({links:[]}));await importing;cases++;
 }
 // An old session's response (success or HTTP error) cannot
 // hide a new authenticated workspace or overwrite its new-session message.
 for(const status of [200,401,500]){
  const f=await fixture(),old=f.run("safely(async()=>{await api('/external-links');message('old success')})"),request=f.requests.shift();
  f.run("show(false);show(true);message('new session')");request.resolve(response({links:[]},status));await old;
  assert.equal(f.el('workspace').hidden,false);assert.equal(f.el('message').textContent,'new session');cases++;
 }
 // An actual 401 in the current session must still close the workspace.
 const g=await fixture(),expired=g.run("safely(()=>api('/external-links'))"),request=g.requests.shift();request.resolve(response({},401));await expired;
 assert.equal(g.el('workspace').hidden,true);assert.equal(g.el('save').disabled,true);assert.match(g.el('message').textContent,/登录状态已过期/);cases++;
 console.log('PASS '+cases+' lifecycle UI cases');
})().catch(e=>{console.error(e);process.exitCode=1});
`
