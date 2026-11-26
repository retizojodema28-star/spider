#!/usr/bin/env python3
"""Independent Worker/MySQL acceptance. Mutates ONLY apk-html-go-acceptance:18084.

Requires the parent's explicit release in docs/worker-test-handoff.md. Reads the
env file without displaying credentials. Uses only Python's standard library,
Docker Compose, and the MySQL client already in the disposable MySQL container.
All assertions remain enabled under python -O. Failures produce a nonzero exit
and evidence JSON; cleanup always restores the normal worker and services.
"""

import argparse
from concurrent.futures import ThreadPoolExecutor
import copy
from datetime import datetime, timezone
import hashlib
import http.cookiejar
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request


ROOT = Path(__file__).resolve().parents[1]
PROJECT = "apk-html-go-acceptance"
BASE = "http://127.0.0.1:18084"
JOBS = "/admin/api/lan/persist/jobs"
HANDOFF = ROOT / "docs/worker-test-handoff.md"


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def read_env(path):
    values = {}
    for line in path.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        key, separator, value = line.partition("=")
        if separator:
            value = value.strip()
            if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
                value = value[1:-1]
            values[key.strip()] = value
    return values


class Client:
    def __init__(self):
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(
            urllib.request.ProxyHandler({}),
            urllib.request.HTTPCookieProcessor(self.jar))
        self.csrf = ""

    def clone(self):
        client = Client()
        client.csrf = self.csrf
        for cookie in self.jar:
            client.jar.set_cookie(copy.copy(cookie))
        return client

    def request(self, path, method="GET", body=None, expected=(200,), ua="Mozilla/5.0"):
        headers = {"Origin": BASE, "X-CSRF-Token": self.csrf,
                   "User-Agent": ua, "Accept": "application/json"}
        data = None
        if body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        req = urllib.request.Request(BASE + path, data=data, headers=headers, method=method)
        try:
            response = self.opener.open(req, timeout=20)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw, status, response_headers = response.read(), response.code, dict(response.headers)
        # Never include response bodies: session/login responses contain secrets.
        require(status in expected, "%s %s returned %d; expected %s" % (method, path, status, expected))
        return status, raw, response_headers

    def api(self, path, method="GET", body=None, expected=(200,)):
        status, raw, _ = self.request(path, method, body, expected)
        return status, json.loads(raw)

    def login(self, env):
        self.csrf = self.api("/admin/api/session")[1]["data"]["csrf_token"]
        self.csrf = self.api("/admin/api/session/login", "POST", {
            "username": env["ADMIN_USERNAME"], "password": env["ADMIN_PASSWORD"]
        })[1]["data"]["csrf_token"]


class Acceptance:
    def __init__(self, args):
        self.args = args
        self.env = read_env(args.env_file)
        require(self.env.get("APP_PORT") == "18084", "APP_PORT must be 18084; preview is forbidden")
        require(args.env_file.resolve() == Path("/tmp/apk-html-go-acceptance.env").resolve(),
                "only the disposable acceptance env file is permitted")
        lines = HANDOFF.read_text().splitlines()
        release = max((i for i, line in enumerate(lines) if line == "PARENT_READY_FOR_WORKER_MUTATIONS"), default=-1)
        complete = max((i for i, line in enumerate(lines) if line == "WORKER_MUTATIONS_COMPLETE"), default=-1)
        require(release > complete, "parent must explicitly release acceptance before each run")
        self.compose_args = ["docker", "compose", "--env-file", str(args.env_file.resolve()),
                             "-p", PROJECT, "-f", str(ROOT / "deployments/docker/docker-compose.yaml")]
        self.client = Client()
        self.results, self.commands, self.owned_jobs = [], [], []
        self.mutated = False
        self.override = None
        self.started = datetime.now(timezone.utc).isoformat()

    def redact(self, value):
        for key, secret in self.env.items():
            if secret and any(word in key for word in ("PASSWORD", "SECRET", "TOKEN", "DSN")):
                value = value.replace(secret, "[REDACTED]")
        return value

    def compose(self, *args, override=False, stdin=None):
        argv = self.compose_args[:]
        if override:
            argv.extend(["-f", str(self.override)])
        argv.extend(args)
        start = time.monotonic()
        proc = subprocess.run(argv, input=stdin, text=True, capture_output=True, cwd=ROOT, timeout=90)
        # SQL and container exec bodies are never placed in the command report.
        label = "compose " + ("exec mysql [SQL via stdin]" if args[0] == "exec" else " ".join(args))
        if override:
            label += " [LANGUAGE_DATA_DIR=/missing override]"
        self.commands.append({"command": label, "exit_code": proc.returncode,
                              "seconds": round(time.monotonic() - start, 3)})
        require(proc.returncode == 0, label + " failed: " + self.redact(proc.stderr[-1200:]))
        return proc.stdout

    def sql(self, query):
        output = self.compose("exec", "-T", "mysql", "sh", "-c",
                              'MYSQL_PWD="$MYSQL_PASSWORD" exec mysql --user="$MYSQL_USER" '
                              '--database="$MYSQL_DATABASE" --batch --skip-column-names --raw',
                              stdin=query + "\n")
        return [json.loads(line) for line in output.splitlines() if line.strip()]

    def state(self):
        revision = self.sql("SELECT JSON_OBJECT('revision',revision) FROM runtime_revisions WHERE id=1;")[0]
        versions = self.sql("SELECT JSON_OBJECT('language',language,'job_id',job_id,'sha256',SHA2(payload,256),"
                            "'words',JSON_LENGTH(JSON_EXTRACT(payload,'$.words')),'sentences',"
                            "JSON_LENGTH(JSON_EXTRACT(payload,'$.sentences'))) FROM language_versions ORDER BY language;")
        return {"revision": revision["revision"], "versions": versions}

    def job(self, job_id):
        require(re.fullmatch(r"[a-f0-9]{64}", job_id) is not None, "invalid generated job ID")
        rows = self.sql("SELECT JSON_OBJECT('id',id,'status',status,'phase',phase,'attempts',attempts,"
                        "'max_attempts',max_attempts,'lease_owner',lease_owner,'lease_until',lease_until,"
                        "'last_error',last_error,'result',result,'started_at',started_at,'finished_at',finished_at) "
                        "FROM refresh_jobs WHERE id='%s';" % job_id)
        require(len(rows) == 1, "persisted job missing")
        return rows[0]

    def events(self, job_id):
        require(re.fullmatch(r"[a-f0-9]{64}", job_id) is not None, "invalid generated job ID")
        return self.sql("SELECT JSON_OBJECT('id',id,'job_id',job_id,'attempt',attempt,'status',status,"
                        "'message',message,'created_at',created_at) FROM refresh_job_events "
                        "WHERE job_id='%s' ORDER BY id ASC;" % job_id)

    def retained_diagnostics(self, job_id, failed, done):
        events = self.events(job_id)
        require(any(event["status"] == failed["status"] and event["attempt"] == failed["attempts"] and
                    event["message"] == failed["last_error"] and event["created_at"] for event in events),
                "successful retry erased the original failure diagnostic")
        require(any(event["status"] == "done" and event["attempt"] == done["attempts"] for event in events),
                "successful retry did not retain completion history")
        exposed = self.client.api(JOBS + "?job_id=" + job_id)[1]["data"]["events"]
        require({event["id"] for event in exposed} == {event["id"] for event in events} and
                all(event["job_id"] == job_id for event in exposed),
                "admin API does not expose the persisted job diagnostics")
        require(any(event["message"] == failed["last_error"] for event in exposed),
                "admin API erased prior failure details after success")
        return events

    def poll(self, predicate, description, timeout=35):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            result = predicate()
            if result:
                return result
            time.sleep(0.25)
        raise AssertionError("timed out: " + description)

    def healthy(self, *services):
        def ready():
            raw = self.compose("ps", "--format", "json")
            parsed = json.loads(raw) if raw.strip().startswith("[") else [json.loads(line) for line in raw.splitlines() if line.strip()]
            rows = {row["Service"]: row for row in parsed}
            return all(name in rows and rows[name].get("State") == "running" and
                       rows[name].get("Health") == "healthy" for name in services)
        self.poll(ready, "healthy " + ",".join(services), timeout=65)

    def enqueue(self, attempts=3):
        _, value = self.client.api(JOBS, "POST", {"languages": ["id"], "max_attempts": attempts}, (202,))
        job_id = value["job_id"]
        require(value["status"] == "queued" and value["job"]["attempts"] == 0, "202 must describe queued job")
        self.owned_jobs.append(job_id)
        return job_id

    def wait_job(self, job_id, status, timeout=35):
        def ready():
            value = self.job(job_id)
            if value["status"] == status:
                return value
            if value["status"] in ("done", "failed"):
                raise AssertionError("job reached %s instead of %s: %s" % (value["status"], status, value["last_error"]))
            return None
        return self.poll(ready, "job " + status, timeout)

    def public(self):
        status, raw, headers = self.client.request("/apps/worker-acceptance?lan=id")
        return {"status": status, "sha256": hashlib.sha256(raw).hexdigest(),
                "revision": int(headers["X-Runtime-Revision"])}

    def observed_public(self, revision):
        return self.poll(lambda: self.public() if self.public()["revision"] == revision else None,
                         "web observes publication revision")

    def normal_worker(self):
        self.compose("up", "-d", "--no-deps", "--no-build", "--force-recreate", "worker")
        self.healthy("worker")

    def capture_worker_log(self, stage):
        output = self.compose("logs", "--no-color", "--timestamps", "worker")
        path = self.args.output.with_name(self.args.output.stem + "-" + stage + ".log")
        path.write_text(self.redact(output))
        return str(path)

    def failing_worker(self):
        self.compose("up", "-d", "--no-deps", "--no-build", "--force-recreate", "worker", override=True)
        self.healthy("worker")

    def check(self, name, function):
        started = time.monotonic()
        try:
            evidence = function()
            result = {"name": name, "passed": True, "evidence": evidence}
            print("PASS", name, flush=True)
        except Exception as error:
            result = {"name": name, "passed": False, "error": self.redact(str(error))}
            print("FAIL", name, result["error"], flush=True)
        result["seconds"] = round(time.monotonic() - started, 3)
        self.results.append(result)
        self.save()
        return result["passed"]

    def independent_success(self):
        before = self.state()
        self.compose("stop", "worker")
        job_id = self.enqueue()
        time.sleep(1.2)
        require(self.job(job_id)["status"] == "queued", "web executed refresh while worker stopped")
        self.client.api(JOBS, "POST", {"languages": ["id"], "force": True}, (409,))
        self.compose("stop", "web")
        self.compose("start", "worker")
        done = self.wait_job(job_id, "done")
        after = self.state()
        require(done["attempts"] == 1 and not done["lease_owner"] and done["lease_until"] is None and
                done["finished_at"] is not None and not done["last_error"], "completed lease/state incorrect")
        result = json.loads(done["result"])
        version = next(v for v in after["versions"] if v["language"] == "id")
        require(version["job_id"] == job_id and version["words"] > 0 and version["sentences"] > 0,
                "worker did not persist a nonempty language version")
        require(result["id"] == {"words": version["words"], "sentences": version["sentences"]},
                "persisted payload disagrees with result JSON")
        require(after["revision"] == before["revision"] + 1, "publication must increment revision exactly once")
        self.compose("start", "web")
        self.healthy("web", "worker")
        public = self.observed_public(after["revision"])
        api_job = self.client.api(JOBS + "?job_id=" + job_id)[1]["data"]["jobs"][0]
        require(api_job["status"] == "done" and "lease_owner" not in api_job, "API persistence or owner redaction incorrect")
        return {"job_id": job_id, "web_stopped_during_completion": True, "before_revision": before["revision"],
                "after": after, "public": public}

    def failure_and_retry(self):
        before, old_public = self.state(), self.public()
        self.failing_worker()
        job_id = self.enqueue(1)
        failed = self.wait_job(job_id, "failed")
        require(failed["attempts"] == 1 and "missing" in failed["last_error"] and failed["finished_at"] is not None,
                "missing directory did not produce durable terminal failure")
        require(self.state() == before and self.public() == old_public, "failure replaced last known good bank/snapshot")
        failure_log = self.capture_worker_log("manual-failure")
        self.normal_worker()
        self.client.api(JOBS + "/" + job_id + "/retry", "POST", {})
        done = self.wait_job(job_id, "done")
        after = self.state()
        require(done["attempts"] == 1 and not done["last_error"], "manual retry did not reset attempts/error")
        require(after["revision"] == before["revision"] + 1, "retry publication revision incorrect")
        require(next(v for v in after["versions"] if v["language"] == "id")["job_id"] == job_id,
                "retry did not publish its language version")
        events = self.retained_diagnostics(job_id, failed, done)
        self.observed_public(after["revision"])
        self.client.api(JOBS + "/" + job_id + "/retry", "POST", {}, (404,))
        return {"job_id": job_id, "failed_error": failed["last_error"], "failed_job": failed,
                "completed_job": done, "events_after_success": events,
                "failure_container_log": failure_log, "old_public_preserved": True,
                "recovered_revision": after["revision"]}

    def automatic_retry(self):
        before = self.state()
        self.failing_worker()
        job_id = self.enqueue(3)
        retry = self.wait_job(job_id, "retry")
        self.compose("stop", "worker")
        require(retry["attempts"] == 1 and retry["finished_at"] is None, "first failure must schedule retry")
        require(self.state() == before, "retry published a failed bank")
        failure_log = self.capture_worker_log("automatic-failure")
        self.client.api(JOBS + "/" + job_id + "/retry", "POST", {}, (409,))
        self.normal_worker()
        done = self.wait_job(job_id, "done")
        require(done["attempts"] == 2, "automatic retry should recover on second attempt")
        require(self.state()["revision"] == before["revision"] + 1, "automatic retry revision incorrect")
        events = self.retained_diagnostics(job_id, retry, done)
        self.observed_public(before["revision"] + 1)
        return {"job_id": job_id, "failed_attempts": retry["attempts"], "completed_attempts": done["attempts"],
                "retry_job": retry, "completed_job": done, "events_after_success": events,
                "failure_container_log": failure_log}

    def concurrent_workers(self):
        before = self.state()
        self.compose("up", "-d", "--no-deps", "--no-build", "--scale", "worker=2", "worker")
        try:
            def both_healthy():
                raw = self.compose("ps", "--format", "json", "worker")
                rows = json.loads(raw) if raw.strip().startswith("[") else [json.loads(line) for line in raw.splitlines() if line.strip()]
                return len(rows) == 2 and all(row.get("Health") == "healthy" for row in rows)
            self.poll(both_healthy, "two healthy independent workers", timeout=65)
            job_id = self.enqueue(3)
            done = self.wait_job(job_id, "done")
            require(done["attempts"] == 1 and self.state()["revision"] == before["revision"] + 1,
                    "competing MySQL workers claimed or published more than once")
            self.observed_public(before["revision"] + 1)
        finally:
            self.compose("up", "-d", "--no-deps", "--no-build", "--scale", "worker=1", "worker")
            self.healthy("worker")
        return {"workers": 2, "job_id": job_id, "attempts": done["attempts"]}

    def concurrent_admission(self):
        self.compose("stop", "worker")
        clients = [self.client.clone() for _ in range(8)]
        def submit(client):
            return client.api(JOBS, "POST", {"languages": ["id"], "max_attempts": 3}, (202, 409))
        with ThreadPoolExecutor(max_workers=8) as pool:
            replies = list(pool.map(submit, clients))
        accepted = [reply[1]["job_id"] for reply in replies if reply[0] == 202]
        self.owned_jobs.extend(accepted)
        require(len(accepted) == 1, "concurrent MySQL admission accepted %d jobs, want one" % len(accepted))
        before = self.state()
        self.compose("restart", "web")
        self.healthy("web")
        require(self.job(accepted[0])["status"] == "queued", "queued job did not survive web restart")
        self.compose("start", "worker")
        done = self.wait_job(accepted[0], "done")
        require(done["attempts"] == 1 and self.state()["revision"] == before["revision"] + 1,
                "queued job was executed more than once")
        self.observed_public(before["revision"] + 1)
        return {"accepted": 1, "conflicts": 7, "job_id": accepted[0], "queued_restart_durable": True}

    def expired_lease(self):
        self.compose("stop", "worker")
        before = self.state()
        job_id = self.enqueue(3)
        self.sql("UPDATE refresh_jobs SET status='running',phase='read_local_files',attempts=1,"
                 "lease_owner='acceptance-crashed-worker',lease_until=UTC_TIMESTAMP(3)+INTERVAL 1 MINUTE,"
                 "started_at=UTC_TIMESTAMP(3)-INTERVAL 2 MINUTE WHERE id='%s';" % job_id)
        self.compose("start", "worker")
        self.healthy("worker")
        time.sleep(1.2)
        leased = self.job(job_id)
        require(leased["status"] == "running" and leased["attempts"] == 1 and
                leased["lease_owner"] == "acceptance-crashed-worker" and self.state() == before,
                "worker stole an unexpired lease")
        self.client.api(JOBS, "POST", {"languages": ["id"], "force": True}, (409,))
        self.compose("stop", "worker")
        self.sql("UPDATE refresh_jobs SET lease_until=UTC_TIMESTAMP(3)-INTERVAL 1 MINUTE WHERE id='%s';" % job_id)
        self.compose("start", "worker")
        done = self.wait_job(job_id, "done")
        require(done["attempts"] == 2 and not done["lease_owner"] and done["lease_until"] is None,
                "expired lease was not reclaimed exactly once")
        require(self.state()["revision"] == before["revision"] + 1, "lease recovery publication incorrect")
        self.observed_public(before["revision"] + 1)
        return {"job_id": job_id, "live_lease_preserved": True, "attempts": done["attempts"], "revision": before["revision"] + 1}

    def restart_durability(self):
        before, public = self.state(), self.public()
        jobs = [self.job(job_id) for job_id in self.owned_jobs]
        events = [self.events(job_id) for job_id in self.owned_jobs]
        self.compose("restart", "web", "worker")
        self.healthy("web", "worker")
        require(self.state() == before, "restart changed persisted bank/revision")
        require(self.observed_public(before["revision"]) == public, "restart changed public snapshot")
        require([self.job(job_id) for job_id in self.owned_jobs] == jobs, "restart changed terminal jobs")
        require([self.events(job_id) for job_id in self.owned_jobs] == events, "restart lost attempt diagnostics")
        self.client.api(JOBS)  # The pre-restart admin session must remain valid.
        return {"revision": before["revision"], "terminal_jobs_preserved": len(jobs),
                "diagnostic_events_preserved": sum(len(rows) for rows in events), "public": public}

    def public_database_outage(self):
        paths = ["/", "/apps", "/apps/worker-acceptance?lan=id", "/category/game/", "/robots.txt", "/sitemap.xml"]
        baseline = {path: self.client.request(path)[1:] for path in paths}
        self.compose("stop", "worker")
        self.compose("stop", "mysql")
        try:
            started = time.monotonic()
            for path in paths:
                for ua in ("Mozilla/5.0", "Googlebot"):
                    _, raw, headers = self.client.request(path, ua=ua)
                    require(raw == baseline[path][0] and headers.get("X-Runtime-Revision") ==
                            baseline[path][1].get("X-Runtime-Revision"), "DB outage changed " + path)
                _, raw, _ = self.client.request(path, "HEAD")
                require(raw == b"", "HEAD emitted body")
            elapsed = time.monotonic() - started
            self.client.request("/healthz", expected=(503,))
        finally:
            self.compose("start", "mysql")
            self.healthy("mysql")
            self.compose("start", "worker")
            self.healthy("web", "worker")
        return {"get_requests": len(paths) * 2, "head_requests": len(paths), "seconds": round(elapsed, 3),
                "note": "Live DB-outage independence; exact zero-query proof is TestWorkerReviewPublicRoutesNeverQueryDatabaseAfterPublication."}

    def exhausted_lease(self):
        self.compose("stop", "worker")
        before = self.state()
        job_id = self.enqueue(1)
        self.sql("UPDATE refresh_jobs SET status='running',phase='read_local_files',attempts=1,"
                 "lease_owner='acceptance-exhausted-worker',lease_until=UTC_TIMESTAMP(3)-INTERVAL 1 MINUTE,"
                 "started_at=UTC_TIMESTAMP(3)-INTERVAL 2 MINUTE WHERE id='%s';" % job_id)
        self.compose("start", "worker")
        self.healthy("worker")
        time.sleep(2)
        got = self.job(job_id)
        # Preserve the failing state even though fixture cleanup must unblock the project.
        self.results.append({"name": "exhausted lease observed database state", "evidence": got})
        self.save()
        try:
            require(got["status"] == "failed" and got["finished_at"] is not None and
                    not got["lease_owner"] and got["lease_until"] is None and "expired" in got["last_error"],
                    "expired final attempt remains %s after healthy worker ticks; attempts=%d finished_at=%s" %
                    (got["status"], got["attempts"], got["finished_at"]))
            require(self.state() == before, "exhausted lease changed bank/revision")
            followup = self.enqueue(1)
            self.wait_job(followup, "done")
        finally:
            if got["status"] == "running":
                self.sql("UPDATE refresh_jobs SET status='failed',phase='finished',lease_owner='',lease_until=NULL,"
                         "finished_at=UTC_TIMESTAMP(3),last_error='acceptance fixture cleanup after captured expired-lease failure' "
                         "WHERE id='%s' AND status='running';" % job_id)
        return {"job_id": job_id, "status": got["status"]}

    def save(self):
        self.args.output.parent.mkdir(parents=True, exist_ok=True)
        data = {"project": PROJECT, "base_url": BASE, "started_at": self.started,
                "checks": self.results, "commands": self.commands,
                "passed": sum(row.get("passed") is True for row in self.results),
                "failed": sum(row.get("passed") is False for row in self.results)}
        self.args.output.write_text(self.redact(json.dumps(data, indent=2)) + "\n")

    def run(self):
        self.healthy("mysql", "web", "worker")
        self.client.login(self.env)
        active = self.sql("SELECT JSON_OBJECT('n',COUNT(*)) FROM refresh_jobs WHERE status IN ('queued','running','retry');")[0]["n"]
        require(active == 0, "acceptance has an existing active job; refusing to interfere")
        with tempfile.TemporaryDirectory(prefix="apk-worker-acceptance-") as temporary:
            self.override = Path(temporary) / "missing-language.json"
            self.override.write_text(json.dumps({"services": {"worker": {"environment": {"LANGUAGE_DATA_DIR": "/missing"}}}}))
            self.mutated = True
            try:
                for name, test in (
                    ("independent worker publishes with web stopped", self.independent_success),
                    ("failure preserves snapshot; manual retry recovers", self.failure_and_retry),
                    ("automatic retry recovers after missing files", self.automatic_retry),
                    ("MySQL concurrent admission and queued-job durability", self.concurrent_admission),
                    ("two independent MySQL workers publish exactly once", self.concurrent_workers),
                    ("expired nonfinal lease recovers", self.expired_lease),
                    ("web/worker restart preserves jobs, versions and public snapshot", self.restart_durability),
                    ("public routes survive actual MySQL outage", self.public_database_outage),
                    ("expired final lease becomes terminal without another job", self.exhausted_lease),
                ):
                    self.check(name, test)
            finally:
                restored = self.check("restore normal acceptance services", self.restore)
                self.save()
                if restored:
                    with HANDOFF.open("a") as handoff:
                        handoff.write("\nWORKER_MUTATIONS_COMPLETE\n\nNormal acceptance web/worker/MySQL restored healthy. Parent may rebuild/redeploy. "
                                      "Worker report: `%s`; passed=%d failed=%d.\n" % (
                                          self.args.output, sum(row.get("passed") is True for row in self.results),
                                          sum(row.get("passed") is False for row in self.results)))
        return 1 if any(row.get("passed") is False for row in self.results) else 0

    def restore(self):
        self.compose("start", "mysql", "web")
        self.healthy("mysql", "web")
        self.normal_worker()
        self.healthy("mysql", "web", "worker")
        return {"normal_language_root": "/app/data/lan", "all_services_healthy": True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env-file", type=Path, default=Path("/tmp/apk-html-go-acceptance.env"))
    parser.add_argument("--output", type=Path, default=Path("/tmp/apk-worker-acceptance.json"))
    args = parser.parse_args()
    runner = None
    try:
        runner = Acceptance(args)
        return runner.run()
    except Exception as error:
        message = str(error) if runner is None else runner.redact(str(error))
        print("ERROR", message, file=sys.stderr)
        if runner is not None:
            runner.results.append({"name": "acceptance harness", "passed": False, "error": message})
            runner.save()
        return 1


if __name__ == "__main__":
    sys.exit(main())
