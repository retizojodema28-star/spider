package site

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Verify the delivered admin JavaScript translates API failures at the UI
// boundary and documents JSON keys without changing the keys or business text.
func TestAdminTemplateLocalizesErrorsAndDocumentsJSONFields(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to execute the delivered admin JavaScript")
	}
	html, err := os.ReadFile("templates/admin.html")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.SplitN(strings.SplitN(string(html), "<script>", 2)[1], "</script>", 2)[0]
	cmd := exec.Command(node, "-e", adminUILocalizationJS)
	cmd.Stdin = bytes.NewBufferString(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("actual admin JS localization assertions: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte("PASS admin UI localization")) {
		t.Fatalf("incomplete admin UI localization assertions: %s", out)
	}
}

const adminUILocalizationJS = `
const fs=require('fs'),vm=require('vm'),assert=require('node:assert/strict');
const source=fs.readFileSync(0,'utf8'),elements=new Map(),requests=[];
function el(id){if(!elements.has(id))elements.set(id,{value:id==='siteKey'?'default':'',hidden:false,textContent:'',className:'',disabled:false,append(){},replaceChildren(){},add(){}});return elements.get(id)}
const response=(body,status=200)=>({ok:status<400,status,json:async()=>body});
const ctx=vm.createContext({document:{getElementById:el,createElement:()=>({textContent:'',onclick:null})},setInterval(){},Option:function(){},confirm:()=>true,fetch:async(url,opt)=>{
 if(url.endsWith('/session'))return response({data:{authenticated:false,csrf_token:'probe'}});
 return new Promise(resolve=>requests.push({url,opt,resolve}));
}});
const run=s=>vm.runInContext(s,ctx);
(async()=>{
 run(source);await new Promise(setImmediate);await new Promise(setImmediate);
 async function rejects(message,status,expected){
  const pending=run("api('/probe')"),request=requests.shift();request.resolve(response({message},status));
  await assert.rejects(pending,e=>{assert.equal(e.message,expected);return true});
 }
 await rejects('invalid domain',422,'域名格式无效。');
 await rejects('unsupported language "xx"',422,'不支持的语言代码：xx。');
 await rejects('invalid admin data: unknown hub config field mystery',422,'不支持的 Hub 配置字段：mystery。');
 await rejects('backend leaked English detail',422,'提交的数据未通过校验，请检查字段说明及输入范围。');
 await rejects('backend leaked English detail',500,'服务暂时不可用，请稍后重试。');
 run("selected='external';$('siteKey').value='default'");
 const loading=run('load()'),request=requests.shift();
 request.resolve(response({data:{links:[{id:1,site_key:'default',domain:'business.example',lang:'id',anchor_text:'原始锚文本',target_url:'/原始路径',php_id:7}]}}));
 await loading;
 assert.match(el('fieldHelp').textContent,/target_url：目标地址/);
 assert.match(el('fieldHelp').textContent,/php_id：旧系统逻辑编号，只读/);
 run("$('records').value='0';$('records').onchange()");
 assert.match(el('editor').value,/原始锚文本/);
 assert.match(el('editor').value,/anchor_text/);
 assert.match(el('editor').value,/target_url/);
 assert.match(el('editor').value,/原始路径/,'localization must not rewrite business values');
 console.log('PASS admin UI localization');
})().catch(e=>{console.error(e);process.exitCode=1});
`
