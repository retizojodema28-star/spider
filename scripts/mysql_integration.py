#!/usr/bin/env python3
"""Run the optional MySQL Go tests in an ephemeral container on the test network.
No database password is printed; test-created tables use a unique prefix and are
removed by the Go test. Does not touch the preview database or PHP containers.
"""
import argparse
import os
from pathlib import Path
import subprocess

p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--env-file',type=Path,default=Path('/tmp/apk-html-go-acceptance.env'))
p.add_argument('--output',type=Path,default=Path('docs/evidence/mysql-go-integration.log'))
a=p.parse_args()
root=Path(__file__).resolve().parents[1]
env=dict(line.split('=',1) for line in a.env_file.read_text().splitlines() if '=' in line and not line.startswith('#'))
runenv=os.environ.copy()
runenv['TEST_MYSQL_DSN']=f"apkhtml:{env['MYSQL_PASSWORD']}@tcp(mysql:3306)/apkhtml?charset=utf8mb4&parseTime=True&loc=UTC&timeout=5s&readTimeout=10s&writeTimeout=10s"
module_cache=Path.home()/'go/pkg/mod'
a.output.parent.mkdir(parents=True,exist_ok=True)
cmd=['docker','run','--rm','--network','apk-html-go-acceptance_default','-v',f'{root}:/src:ro','-v',f'{module_cache}:/go/pkg/mod:ro','-v','apk-html-go-test-build-cache:/root/.cache/go-build','-w','/src','-e','TEST_MYSQL_DSN','-e','GOTOOLCHAIN=local','golang:1.25.0','go','test','-mod=readonly','./internal/site','-run','TestAdminOptionalMySQL','-count=1','-v']
with a.output.open('w') as out:r=subprocess.run(cmd,env=runenv,stdout=out,stderr=subprocess.STDOUT)
print(('PASS' if r.returncode==0 else 'FAIL')+' MySQL Go integration; report '+str(a.output))
raise SystemExit(r.returncode)
