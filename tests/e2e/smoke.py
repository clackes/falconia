#!/usr/bin/env python3
"""End-to-end smoke test for a running Falconia instance.

It plays the product's central story: one person's agents (a Codex-like MCP
client, a Claude Code-like one, a Gemini CLI-like CLI user) build and fix an
artifact together, the artifact is shared Google-Docs style, a second person
and an anonymous visitor open it, and every step keeps the right provenance.

    docker compose up -d
    python3 tests/e2e/smoke.py

Configure with FALCONIA_API (default http://localhost:8080) and
FALCONIA_CONTENT (default http://localhost:8081). Uses only the standard
library, and creates uniquely named users so it can run again and again.
"""
import io
import json
import os
import sys
import urllib.error
import urllib.request
import uuid
import zipfile

API = os.environ.get("FALCONIA_API", "http://localhost:8080").rstrip("/")
CONTENT = os.environ.get("FALCONIA_CONTENT", "http://localhost:8081").rstrip("/")
RUN = uuid.uuid4().hex[:8]

def call(method, path, body=None, token=None, headers=None, raw=False, base=None):
    h={"Content-Type":"application/json"}
    if token: h["Authorization"]="Bearer "+token
    h.update(headers or {})
    data=None if body is None else (body if isinstance(body,bytes) else json.dumps(body).encode())
    req=urllib.request.Request((base or API)+path, data=data, method=method, headers=h)
    try:
        r=urllib.request.urlopen(req); b=r.read(); st=r.status; hd=dict(r.headers)
    except urllib.error.HTTPError as e:
        b=e.read(); st=e.code; hd=dict(e.headers)
    if raw: return st,b,hd
    try: return st, json.loads(b) if b else None, hd
    except Exception: return st, b.decode(errors="replace"), hd

try:
    _st, _h, _ = call("GET", "/api/health")
    assert _st == 200 and isinstance(_h, dict) and _h.get("ok")
except Exception:
    sys.exit(f"No Falconia API at {API} (set FALCONIA_API / FALCONIA_CONTENT to your dev ports)")

ok=0; fail=0
def check(name, cond, extra=""):
    global ok, fail
    if cond: ok+=1; print("  ✓", name)
    else: fail+=1; print("  ✗", name, extra)

# users
st,a,_=call("POST","/api/auth/register",{"email":f"alice-{RUN}@example.com","password":"password-alice","display_name":"Alice"})
if st==409: st,a,_=call("POST","/api/auth/login",{"email":f"alice-{RUN}@example.com","password":"password-alice"})
alice=a["token"]
st,b,_=call("POST","/api/auth/register",{"email":f"bob-{RUN}@example.com","password":"password-bob1","display_name":"Bob"})
if st==409: st,b,_=call("POST","/api/auth/login",{"email":f"bob-{RUN}@example.com","password":"password-bob1"})
bob=b["token"]
st,k,_=call("POST","/api/keys",{"label":"codex","kind":"mcp"},alice)
key=k["token"]; check("new key prefix falconia_pat_", key.startswith("falconia_pat_"), key[:15])

# MCP: initialize as codex
st,r,hd=call("POST","/mcp",{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"codex-mcp-client","version":"0.50"}}},key)
sid=hd.get("Mcp-Session-Id")
check("mcp initialize negotiates 2025-06-18", r["result"]["protocolVersion"]=="2025-06-18")
check("mcp session id returned", bool(sid))
st,r,_=call("POST","/mcp",{"jsonrpc":"2.0","method":"notifications/initialized"},key,{"Mcp-Session-Id":sid},raw=True)
check("notification gets 202", st==202, st)
st,r,_=call("POST","/mcp",{"jsonrpc":"2.0","id":2,"method":"tools/list"},key,{"Mcp-Session-Id":sid})
names=[t["name"] for t in r["result"]["tools"]]
check("tools include get_file & rotate_link", "get_file" in names and "rotate_link" in names)
st,r,_=call("POST","/mcp",None,"falconia_pat_bad_key",raw=True)
check("bad key -> 401", st==401, st)

def tool(name,args,sess=sid,tok=key,hdrs=None):
    h={"Mcp-Session-Id":sess} if sess else {}
    h.update(hdrs or {})
    st,r,_=call("POST","/mcp",{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":name,"arguments":args}},tok,h)
    res=r["result"]; txt=res["content"][0]["text"]
    return res.get("isError"), (json.loads(txt) if not res.get("isError") else txt)

err,art=tool("create_artifact",{"title":"Sales dashboard","files":[
  {"path":"index.html","content":"<!doctype html><html><head><link rel=stylesheet href=style.css></head><body><h1>Sales</h1><script src=app.js></script></body></html>"},
  {"path":"style.css","content":"h1{color:red}"},
  {"path":"app.js","content":"console.log(1)"}]})
check("create bundle via MCP", not err, art)
aid=art["id"]
check("agents = [codex]", art["agents"]==["codex"], art.get("agents"))
# deploy only css, as claude-code via ?agent
st,r,_=call("POST","/mcp?agent=claude-code",{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"deploy_artifact","arguments":{"id":aid,"files":[{"path":"style.css","content":"h1{color:blue}"}],"message":"blue title"}}},key)
d=json.loads(r["result"]["content"][0]["text"])
check("patch deploy keeps other files (v2)", d["latest_version"]==2)
check("agents now claude-code, codex", d["agents"]==["claude-code","codex"], d["agents"])
err,f=tool("get_file",{"id":aid,"path":"app.js"})
check("get_file returns carried-over app.js", not err and f["content"]=="console.log(1)", f)
err,f=tool("get_file",{"id":aid,"path":"style.css"})
check("get_file returns new css", not err and "blue" in f["content"], f)
err,g=tool("get_artifact",{"id":aid})
check("get_artifact has entry code for bundle", not err and "<h1>Sales</h1>" in g.get("code",""), str(g)[:200])
check("get_artifact lists 3 files", len(g.get("files",[]))==3)
# code-only deploy on bundle must keep files
err,d=tool("deploy_artifact",{"id":aid,"code":"<!doctype html><html><head><link rel=stylesheet href=style.css></head><body><h1>Sales v3</h1></body></html>"})
err,g=tool("get_artifact",{"id":aid})
check("code-only deploy keeps bundle files", len(g.get("files",[]))==3 and "v3" in g["code"], g.get("files"))

# versions list agent
st,vs,_=call("GET",f"/api/artifacts/{aid}/versions",None,alice)
check("versions carry agent", [v["agent"] for v in vs["versions"]]==["codex","claude-code","codex"], [v["agent"] for v in vs["versions"]])

# bob cannot see
st,_,_=call("GET",f"/api/artifacts/{aid}",None,bob)
check("bob cannot open restricted artifact", st==404, st)
# link sharing
st,u,_=call("PATCH",f"/api/artifacts/{aid}",{"visibility":"link"},alice)
lp=u["artifact"].get("link_path","")
check("link_path appears", lp.startswith("/p/") and len(lp)>10, lp)
tok=lp[3:]
st,p,_=call("GET","/api/p/"+tok)
check("anonymous can open link", st==200 and p["artifact"]["title"]=="Sales dashboard", st)
check("anonymous info is minimal (no space/visibility)", "space_slug" not in p["artifact"] and "visibility" not in p["artifact"])
st,html,_=call("GET",p["content"]["url"].replace(CONTENT,""),base=CONTENT,raw=True)
check("anonymous content loads with SDK", st==200 and b"__sfa/sdk.js" in html and b"Sales v3" in html, st)
st,css,_=call("GET",p["content"]["url"].replace(CONTENT,"")+"style.css",base=CONTENT,raw=True)
check("bundle asset served", st==200 and b"blue" in css)
# bob opens link while signed in -> viewer, can comment, sees in shared
st,p2,_=call("GET","/api/p/"+tok,None,bob)
check("bob via link gets viewer", p2.get("my_role")=="viewer", p2.get("my_role"))
st,t,_=call("POST",f"/api/artifacts/{aid}/threads",{"body":"Title should be bigger","anchor":{"selector":"h1","quote":"Sales v3"}},bob)
check("bob comments", st==201, st)
tid=t["thread"]["id"]
st,sh,_=call("GET","/api/me/shared",None,bob)
check("artifact in bob's Shared with me", any(x["id"]==aid for x in sh["artifacts"]))
st,_,_=call("GET",f"/api/artifacts/{aid}/download",None,bob,raw=True)
check("viewer cannot download source", st==403, st)
# gemini via CLI header reads & resolves
st,txt,_=call("GET",f"/api/artifacts/{aid}/review",None,key,{"X-Falconia-Agent":"gemini-cli"})
check("review digest english", "Open comments on" in txt and "Title should be bigger" in txt)
st,_,_=call("POST",f"/api/threads/{tid}/replies",{"body":"Fixed in v4"},key,{"X-Falconia-Agent":"gemini-cli"})
st,r,_=call("POST",f"/api/threads/{tid}/status",{"resolved":True},key,{"X-Falconia-Agent":"gemini-cli"})
check("agent (editor key) can resolve", st==200 and r["thread"]["status"]=="resolved", st)
st,th,_=call("GET",f"/api/artifacts/{aid}/threads",None,alice)
check("reply records agent gemini-cli", th["threads"][0]["comments"][-1]["agent"]=="gemini-cli", th["threads"][0]["comments"][-1])
# download zip
st,zb,hd=call("GET",f"/api/artifacts/{aid}/download",None,alice,raw=True)
z=zipfile.ZipFile(io.BytesIO(zb))
check("download zip has 3 files", sorted(z.namelist())==["app.js","index.html","style.css"], z.namelist())
# rotate link
st,u2,_=call("POST",f"/api/artifacts/{aid}/link/rotate",{},alice)
st,_,_=call("GET","/api/p/"+tok)
check("old link dead after rotate", st==404, st)
st,_,_=call("GET",f"/api/artifacts/{aid}",None,bob)
check("bob loses access after rotate", st==404, st)
# back to private kills new link
newtok=u2["artifact"]["link_path"][3:]
call("PATCH",f"/api/artifacts/{aid}",{"visibility":"private"},alice)
st,_,_=call("GET","/api/p/"+newtok)
check("private disables link", st==404, st)
call("PATCH",f"/api/artifacts/{aid}",{"visibility":"link"},alice)
st,_,_=call("GET","/api/p/"+newtok)
check("re-enabling link keeps same token", st==200, st)
# agent filter
st,l,_=call("GET","/api/artifacts?agent=claude-code",None,alice)
check("agent filter finds it", any(x["id"]==aid for x in l["artifacts"]))
st,l,_=call("GET","/api/artifacts?agent=cursor",None,alice)
check("agent filter excludes", not any(x["id"]==aid for x in l["artifacts"]))
# legacy key prefix acceptance check (format only)
st,_,_=call("GET","/api/auth/me",None,"sfa_pat_abc_def")
check("legacy prefix parsed as key (401 not 500)", st==401, st)
# studio commit on bundle keeps files
st,c,_=call("POST",f"/api/artifacts/{aid}/commit",{"code":"<!doctype html><html><body><h1>edited in studio</h1></body></html>","message":"studio"},alice)
st,g,_=call("GET",f"/api/artifacts/{aid}",None,alice)
check("studio commit keeps bundle files", len(g.get("files",[]))==3, g.get("files"))
# batch
st,r,_=call("POST","/mcp",[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/x"}],key)
check("batch returns one response", isinstance(r,list) and len(r)==1, r)
# team role viewer
st,o,_=call("POST","/api/orgs",{"name":f"Growth {RUN}"},alice)
oid=o["org"]["id"]
call("POST",f"/api/orgs/{oid}/members",{"email":f"bob-{RUN}@example.com","role":"member"},alice)
err,ta=tool("create_artifact",{"title":"Team doc","code":"<p>hi</p>","space":o["org"]["space_slug"],"visibility":"org"})
st,g,_=call("GET",f"/api/artifacts/{ta['id']}",None,bob)
check("team member is editor by default", g["artifact"]["my_role"]=="editor", g["artifact"].get("my_role"))
call("PATCH",f"/api/artifacts/{ta['id']}",{"team_role":"viewer"},alice)
st,g,_=call("GET",f"/api/artifacts/{ta['id']}",None,bob)
check("team_role viewer applies", g["artifact"]["my_role"]=="viewer", g["artifact"].get("my_role"))
print(f"\n{ok} passed, {fail} failed"); sys.exit(1 if fail else 0)
