#!/usr/bin/env python3
"""Exercise a disposable MySQL-backed Compose acceptance instance (never production)."""
import argparse
import http.cookiejar
import json
from pathlib import Path
import subprocess
import time
import urllib.request
import urllib.error

class Client:
    def __init__(self, base):
        self.base=base;self.csrf='';self.jar=http.cookiejar.CookieJar()
        self.opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))
    def call(self, path, method='GET', body=None, expected=200, headers=None):
        h={'Accept':'application/json','Origin':self.base,'X-CSRF-Token':self.csrf,**(headers or {})}
        data=None
        if body is not None:data=json.dumps(body).encode();h['Content-Type']='application/json'
        req=urllib.request.Request(self.base+path,data=data,headers=h,method=method)
        try:r=self.opener.open(req,timeout=25)
        except urllib.error.HTTPError as e:r=e
        code=r.code;raw=r.read();value=json.loads(raw) if 'application/json' in r.headers.get('Content-Type','') else raw.decode()
        if code!=expected:raise AssertionError(f'{method} {path}: status {code}, expected {expected}: {str(value)[:200]}')
        return value
    def login(self,env):
        self.csrf=self.call('/admin/api/session')['data']['csrf_token']
        self.csrf=self.call('/admin/api/session/login','POST',{'username':env['ADMIN_USERNAME'],'password':env['ADMIN_PASSWORD']})['data']['csrf_token']

def main():
    ap=argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--env-file',type=Path,required=True);ap.add_argument('--port',type=int,default=18084)
    ap.add_argument('--output',type=Path,default=Path('docs/evidence/admin-acceptance.json'))
    ap.add_argument('--restart',action='store_true');args=ap.parse_args()
    if args.port==18082:ap.error('This test mutates data: use a disposable instance, not preview port 18082.')
    env=dict(line.split('=',1) for line in args.env_file.read_text().splitlines() if '=' in line and not line.startswith('#'))
    base=f'http://127.0.0.1:{args.port}';c=Client(base);checks=[]
    def checked(name):checks.append(name);print('PASS',name)
    c.call('/admin/api/sites/config',expected=401);checked('anonymous API denied')
    c.login(env);checked('bootstrap credentials and session rotation')
    c.call('/admin/api/sites/config','POST',{'site_name':'blocked'},expected=403,headers={'X-CSRF-Token':''})
    c.call('/admin/api/sites/config','POST',{'site_name':'blocked'},expected=403,headers={'Origin':'https://evil.invalid'})
    checked('CSRF and cross-origin writes rejected')
    cfg=c.call('/admin/api/sites/config')['data']['site_config']
    c.call('/admin/api/sites/config','POST',{'default_lang':'zz'},expected=422)
    c.call('/admin/api/sites/config','POST',{'download_base_url':'javascript:alert(1)'},expected=422)
    c.call('/admin/api/sites/config','POST',{'route_prefix':'verify','site_name':'Acceptance persisted'})
    html=c.call('/verifyapps/arbitrary?lan=id')
    assert 'href="http://127.0.0.1/verifycategory/game/"' in html
    checked('settings validated and public memory snapshot reloaded')
    c.call('/admin/api/sites/config','POST',{'route_prefix':'','show_subdomain_panel':True})
    c.call('/admin/api/domain-lang-bindings','POST',{'domain':'acceptance.example','lang':'id'})
    c.call('/admin/api/domain-lang-bindings','POST',{'domain':'invalid.example','lang':'zz'},expected=422)
    assert any(b['domain']=='acceptance.example' for b in c.call('/admin/api/domain-lang-bindings')['data']['bindings'])
    c.call('/admin/api/domain-image-bindings','POST',{'domain':'acceptance.example','image_base_url':'/assets'})
    c.call('/admin/api/domain-image-bindings','POST',{'domain':'bad.example','image_base_url':'/../../etc'},expected=422)
    checked('domain/language/image bindings and validation')
    body={'lang':'id','domain':'acceptance.example','anchor_text':'Acceptance visible link','target_url':'https://acceptance.example/item?q=1','weight':100,'enabled':False}
    link=c.call('/admin/api/main-page-external-links','POST',body)['data']['link'];body['id']=link['id']
    assert link['enabled'] is False
    assert 'Acceptance visible link' not in c.call('/apps/arbitrary?lan=id')
    body['enabled']=True;c.call('/admin/api/main-page-external-links','PUT',body)
    assert 'Acceptance visible link' in c.call('/apps/arbitrary?lan=id')
    checked('main links CRUD/enable/disable zero-value persistence and public output')
    d=c.call('/admin/api/internal-domains','POST',{'domain':'inside.example','title':'Acceptance internal','lang':'id','path':'/apps/test','enabled':True,'weight':50})['data']['link']
    assert '//inside.example/apps/test' in c.call('/apps/arbitrary?lan=id')
    checked('internal-domain panel uses managed links')
    c.call('/admin/api/sites/links','POST',{'type':'nonsense','lang':'id','target_url':'https://example.com'},expected=422)
    c.call('/admin/api/external-links','POST',{'lang':'id','target_url':'javascript:alert(1)'},expected=422)
    checked('invalid link type and unsafe target rejected')
    imported=c.call('/admin/api/main-page-external-links/import','POST',{'strategy':'append','lang':'id','links':'https://import.example/a,Imported A\nhttps://import.example/a,Imported A'})['data']
    assert 'Imported A' in c.call('/apps/arbitrary?lan=id')
    before=c.call('/admin/api/main-page-external-links')['data']['total']
    c.call('/admin/api/main-page-external-links/import','POST',{'strategy':'replace','lang':'id','links':'javascript:alert(1),bad'},expected=422)
    assert c.call('/admin/api/main-page-external-links')['data']['total']==before
    checked('import deduplication and failed-import atomicity')
    c.call('/admin/api/domain-lang-bindings','DELETE',{'domain':'acceptance.example'})
    c.call('/admin/api/domain-image-bindings','DELETE',{'domain':'acceptance.example'})
    c.call('/admin/api/internal-domains','DELETE',{'ids':[d['id']]})
    c.call('/admin/api/main-page-external-links','DELETE',{'ids':[body['id']]})
    assert 'Acceptance visible link' not in c.call('/apps/arbitrary?lan=id')
    checked('binding and link deletion updates snapshots')
    c.call('/admin/api/sites/config','POST',{'show_list_panel':True})
    c.call('/admin/api/hub/config','POST',{'subdomain_count':3})
    assert c.call('/admin/api/sites/config')['data']['site_config']['show_list_panel'] is True
    checked('unrelated Hub edits preserve site panel settings')
    prefixes=c.call('/admin/api/hub/subdomains/refresh','POST',{'domain':'127.0.0.1','dsl':'{s4n2}','count':3})['data']['subdomains']
    assert len(prefixes)==3 and len(set(prefixes))==3
    hub_html=c.call('/link/?lan=id')
    assert all('https://'+prefix+'.127.0.0.1/' in hub_html for prefix in prefixes)
    checked('Hub subdomain configuration persisted and rendered')
    c.call('/admin/api/sites/links/import','POST',{'type':'external','strategy':'replace','effective_date':'2026-11-01','links':'old.example|global|Historical link|https://old.example/|history|100|true|'})
    c.call('/admin/api/sites/links/import','POST',{'type':'external','strategy':'append','effective_date':'2026-11-02','links':'new.example|id|Newer link|https://new.example/|history|100|true|'})
    old_html=c.call('/link/20261101/0?lan=id')
    assert 'https://old.example/' in old_html and 'https://new.example/' not in old_html
    current_html=c.call('/link/20261102/0?lan=id')
    assert 'https://old.example/' in current_html and 'https://new.example/' in current_html
    checked('dated imports select correct historical link boundaries')
    ext=c.call('/admin/api/external-links')['data']['links']
    old=next(row for row in ext if row['target_url']=='https://old.example/')
    c.call('/admin/api/external-links','DELETE',{'ids':[old['id']]})
    after_delete=c.call('/link/20261101/0?lan=id')
    assert 'https://old.example/' not in after_delete and 'https://new.example/' in after_delete
    checked('external deletion rebuilds snapshots like PHP')
    if args.restart:
        subprocess.run(['docker','compose','--env-file',str(args.env_file.resolve()),'-p','apk-html-go-acceptance','-f','deployments/docker/docker-compose.yaml','restart','web','worker'],check=True,stdout=subprocess.DEVNULL)
        for _ in range(60):
            try:
                if c.call('/healthz')['ok']:break
            except Exception:time.sleep(1)
        else:raise AssertionError('restart did not recover')
        assert c.call('/admin/api/sites/config')['data']['site_config']['site_name']=='Acceptance persisted'
        assert c.call('/admin/api/main-page-external-links')['data']['total']==before-1
        assert 'Acceptance visible link' not in c.call('/apps/arbitrary?lan=id')
        checked('restart preserves settings, deletions and authenticated sessions')
    c.call('/admin/api/session/logout','POST',{})
    c.call('/admin/api/sites/config',expected=401);checked('logout revokes session')
    args.output.parent.mkdir(parents=True,exist_ok=True)
    args.output.write_text(json.dumps({'passed':len(checks),'checks':checks,'instance':base,'import_result':imported},ensure_ascii=False,indent=2))
if __name__=='__main__':main()
