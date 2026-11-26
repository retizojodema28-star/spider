package site

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Exercise the actual delivered JS with deliberately out-of-order HTTP responses.
// A late external-links response must never become editable as a language binding.
func TestAdminTemplateDiscardsStaleCollectionLoads(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to execute the delivered admin JavaScript")
	}
	html, err := os.ReadFile("templates/admin.html")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.SplitN(strings.SplitN(string(html), "<script>", 2)[1], "</script>", 2)[0]
	cmd := exec.Command(node, "-e", adminUIAsyncJS)
	cmd.Stdin = bytes.NewBufferString(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("actual admin JS asynchronous requests: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte("PASS 5 asynchronous UI cases")) {
		t.Fatalf("incomplete asynchronous UI assertions: %s", out)
	}
}

const adminUIAsyncJS = `
const fs=require('fs'),vm=require('vm'),assert=require('node:assert/strict');
const source=fs.readFileSync(0,'utf8'), elements=new Map(),deferred=[],writes=[];
function el(id){if(!elements.has(id))elements.set(id,{value:id==='siteKey'?'default':'',hidden:false,textContent:'',className:'',disabled:false,append(){},replaceChildren(){},add(){}});return elements.get(id)}
const response=data=>({ok:true,json:async()=>({data})});
const ctx=vm.createContext({document:{getElementById:el,createElement:()=>({})},setInterval(){},Option:function(){},confirm:()=>true,fetch:async(url,opt)=>{
 if(url.endsWith('/session'))return response({authenticated:false,csrf_token:'probe'});
 if(opt.method==='GET')return new Promise(resolve=>deferred.push({url,resolve}));
 writes.push({url,body:JSON.parse(opt.body)});return response({deleted:1});
}});
const run=s=>vm.runInContext(s,ctx), row=(domain,site_key='default')=>({id:17,domain,site_key,lang:'id',anchor_text:domain,target_url:'https://'+domain+'/'});
async function resolve(request,data){request.resolve(response(data));await Promise.resolve();await Promise.resolve()}
vm.runInContext(source,ctx);
(async()=>{
 await run('safely(session)');
 // Old-tab response arrives after the current tab. Captured DELETE must use
 // the actual binding shown under Domain languages, not the external-link row.
 run("selected='external'");const old=run('load()'),oldRequest=deferred.shift();
 run("selected='languages'");const current=run('load()'),currentRequest=deferred.shift();
 await resolve(currentRequest,{bindings:[row('binding.test')]});await current;
 await resolve(oldRequest,{links:[row('external.test')]});await old;
 run("$('records').value='0';$('records').onchange()");
 assert.equal(run('activeRecord.domain'),'binding.test');
 const removing=run("$('remove').onclick()");await new Promise(setImmediate);
 assert.equal(writes.at(-1).url,'/admin/api/domain-lang-bindings');
 assert.equal(writes.at(-1).body.domain,'binding.test');
 await resolve(deferred.shift(),{bindings:[row('binding.test')]});await removing;
 // While switching, stale records cannot be saved, deleted or imported, even
 // when a caller invokes the actual click handler despite the disabled button.
 run("$('records').value='0';$('records').onchange();selected='external'");
 const switching=run('load()'),switchRequest=deferred.shift(),before=writes.length;
 for(const id of ['save','remove','import']){assert.equal(el(id).disabled,true);await run("$('"+id+"').onclick()");}
 assert.equal(writes.length,before);assert.equal(run('activeRecord'),null);
 await resolve(switchRequest,{links:[row('fresh.test')]});await switching;
 // Same collection, different sites: a late default response must not overwrite
 // preview data or change the namespace of the selected mutation.
 run("$('siteKey').value='default'");const first=run('load()'),a=deferred.shift();
 run("$('siteKey').value='preview'");const second=run('load()'),b=deferred.shift();
 await resolve(b,{links:[row('preview.test','preview')]});await second;
 await resolve(a,{links:[row('default.test')]});await first;
 run("$('records').value='0';$('records').onchange()");
 assert.equal(run('activeRecord.site_key'),'preview');assert.equal(run('activeRecord.domain'),'preview.test');
 // A failed load must not leave the previous collection's editor actionable.
 run("selected='languages'");const failing=run('safely(load)'),bad=deferred.shift();
 bad.resolve({ok:false,status:500,json:async()=>({message:'temporary failure'})});await failing;
 assert.equal(el('save').disabled,true);assert.equal(run('activeRecord'),null);
 // Logout invalidates in-flight list responses too.
 const afterLogout=run('load()'),late=deferred.shift();run('show(false)');
 await resolve(late,{bindings:[row('late.test','preview')]});await afterLogout;
 assert.equal(run('activeRecord'),null);assert.equal(el('save').disabled,true);
 console.log('PASS 5 asynchronous UI cases');
})().catch(e=>{console.error(e);process.exitCode=1});
`
