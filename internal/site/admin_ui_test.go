package site

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Execute the delivered template, rather than a Go copy of its payload logic.
// The small DOM/fetch boundary supplies real API list records; emitted writes are
// replayed through the real authenticated Gin handlers and temporary SQLite DB.
func TestAdminTemplateRecordRoundTripsAndDeleteNamespace(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to execute the delivered admin JavaScript")
	}
	h := newAdminHarness(t, nil)
	if err := h.db.Create(&SiteConfig{SiteKey: "preview", DefaultLanguage: "id"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.rt.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.login()
	h.call("POST", "/admin/api/sites/links/import", map[string]any{"type": "external", "effective_date": "2026-05-14", "links": "https://default.test/,Default"}, 200)
	before, _ := json.Marshal(h.rt.Snapshot().Hub)
	cases := []map[string]any{}
	for _, spec := range []struct{ tab, path, kind string }{
		{"external", "/external-links", "external"}, {"main", "/main-page-external-links", "main-external"}, {"domains", "/internal-domains", "internal-domain"},
	} {
		body := map[string]any{"site_key": "preview", "lang": "id", "domain": "preview.test", "weight": 23, "enabled": true}
		if spec.tab == "domains" {
			body["title"], body["path"] = "Preview", "/"
		} else {
			body["anchor_text"], body["target_url"] = "Preview", "https://preview.test/"+spec.tab
		}
		h.call("POST", "/admin/api"+spec.path, body, 200)
		data := h.call("GET", "/admin/api"+spec.path+"?site_key=preview", nil, 200)["data"].(map[string]any)
		row := data["links"].([]any)[0].(map[string]any)
		if row["php_id"].(float64) != 1 {
			t.Fatalf("list is missing read-only PHPID: %v", row)
		}
		// Unknown response-only fields remain rejected server-side.
		h.call("PUT", "/admin/api"+spec.path, row, 400)
		cases = append(cases, map[string]any{"tab": spec.tab, "row": row})
	}
	html, err := os.ReadFile("templates/admin.html")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.SplitN(strings.SplitN(string(html), "<script>", 2)[1], "</script>", 2)[0]
	input, _ := json.Marshal(map[string]any{"script": script, "cases": cases})
	cmd := exec.Command(node, "-e", adminUIRoundTripJS)
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("actual admin JavaScript: %v\n%s", err, out)
	}
	var result []struct {
		Tab, ZeroMessage string
		Save, Delete     struct {
			URL, Method string
			Body        map[string]any
		}
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("decode captured JavaScript writes: %v: %s", err, out)
	}
	if len(result) != len(cases) {
		t.Fatalf("missing UI round trips: %s", out)
	}
	for _, c := range result {
		if c.Save.Body["site_key"] != "preview" || c.Delete.Body["site_key"] != "preview" || c.Save.Body["id"] == nil {
			t.Fatalf("UI lost the selected record's namespace or SQL handle: %+v", c)
		}
		if _, exists := c.Save.Body["php_id"]; exists {
			t.Fatalf("UI submitted read-only logical ID: %+v", c)
		}
		if !strings.Contains(c.ZeroMessage, "未删除任何记录") {
			t.Fatalf("zero-match UI delete reported success: %q", c.ZeroMessage)
		}
		h.call(c.Save.Method, c.Save.URL, c.Save.Body, 200)
		deleted := h.call(c.Delete.Method, c.Delete.URL, c.Delete.Body, 200)["data"].(map[string]any)["deleted"]
		if deleted != float64(1) {
			t.Fatalf("selected preview row was not deleted: %v", deleted)
		}
	}
	after, _ := json.Marshal(h.rt.Snapshot().Hub)
	if !bytes.Equal(before, after) {
		t.Fatalf("preview UI mutation changed default dated history:\nbefore %s\nafter %s", before, after)
	}
}

const adminUIRoundTripJS = `
const fs=require('fs'),vm=require('vm');
const input=JSON.parse(fs.readFileSync(0,'utf8')), elements=new Map(), writes=[];
let current, deleted=1;
function element(id){if(!elements.has(id))elements.set(id,{value:id==='siteKey'?'preview':'',hidden:false,textContent:'',className:'',append(){},replaceChildren(){},add(){}});return elements.get(id)}
const ctx=vm.createContext({document:{getElementById:element,createElement:()=>({})},setInterval(){},Option:function(){},confirm:()=>true,fetch:async(url,opt)=>{
 const request={URL:url,Method:opt.method,Body:opt.body&&JSON.parse(opt.body)};
 if(opt.method==='PUT'||opt.method==='DELETE')writes.push(request);
 const data=url.endsWith('/session')?{authenticated:false,csrf_token:'test'}:opt.method==='DELETE'?{deleted}: {links:current?[current.row]:[]};
 return {ok:true,status:200,json:async()=>({ok:true,data})};
}});
vm.runInContext(input.script,ctx);
(async()=>{await vm.runInContext('safely(session)',ctx);const results=[];
 for(current of input.cases){
  vm.runInContext('selected='+JSON.stringify(current.tab)+";$('siteKey').value='preview'",ctx);
  await vm.runInContext('load()',ctx);
  vm.runInContext("$('records').value='0';$('records').onchange();$('siteKey').value='default'",ctx);
  await vm.runInContext("$('save').onclick()",ctx);const Save=writes.at(-1);
  vm.runInContext("$('records').value='0';$('records').onchange();$('siteKey').value='default'",ctx);
  deleted=1;await vm.runInContext("$('remove').onclick()",ctx);const Delete=writes.at(-1);
  vm.runInContext("$('records').value='0';$('records').onchange()",ctx);
  deleted=0;await vm.runInContext("$('remove').onclick()",ctx);
  results.push({Tab:current.tab,Save,Delete,ZeroMessage:element('message').textContent});
 }
 process.stdout.write(JSON.stringify(results));
})().catch(e=>{console.error(e);process.exit(1)});
`
