import ast
import asyncio
import builtins
import json
import linecache
import os
import socket
import sys
import traceback
import uuid
import weakref


class ToolError(Exception):
    def __init__(self, tool, message, denied=False):
        self.tool = tool
        self.message = message
        self.denied = bool(denied)
        super().__init__(f"{tool}: {message}" if tool else str(message))


class Step(dict):
    def __getattr__(self, name):
        if name.startswith("__") and name.endswith("__"):
            raise AttributeError(name)
        return self.get(name, None)

    def __setattr__(self, name, value):
        self[name] = value

    def __delattr__(self, name):
        if name in self:
            del self[name]
        else:
            raise AttributeError(name)


class _WorkflowReturn(BaseException):
    pass


class _RPCError(Exception):
    def __init__(self, message, denied=False):
        self.message = message
        self.denied = bool(denied)
        super().__init__(message)


class _BridgeClient:
    def __init__(self, host, port, token):
        self.host = host
        self.port = int(port)
        self.token = token

    def call_sync(self, method, params=None, timeout=30.0):
        payload = json.dumps({
            "token": self.token,
            "method": method,
            "params": params or {},
        }).encode("utf-8") + b"\n"
        with socket.create_connection((self.host, self.port), timeout=timeout) as sock:
            sock.sendall(payload)
            chunks = []
            while True:
                data = sock.recv(65536)
                if not data:
                    break
                nl = data.find(b"\n")
                if nl >= 0:
                    chunks.append(data[:nl])
                    break
                chunks.append(data)
        raw = b"".join(chunks).decode("utf-8", errors="replace").strip()
        if not raw:
            raise _RPCError("empty response from workflow bridge")
        resp = json.loads(raw)
        if "error" in resp and resp["error"] is not None:
            err = resp["error"]
            raise _RPCError(err.get("message", "rpc error"), denied=err.get("denied", False))
        return resp.get("result")

    async def call_async(self, method, params=None):
        payload = json.dumps({
            "token": self.token,
            "method": method,
            "params": params or {},
        }).encode("utf-8") + b"\n"
        reader, writer = await asyncio.open_connection(
            self.host, self.port, limit=64 * 1024 * 1024
        )
        try:
            writer.write(payload)
            await writer.drain()
            line = await reader.readline()
        finally:
            try:
                writer.close()
            except Exception:
                pass
        if not line:
            raise _RPCError("connection closed by workflow bridge")
        resp = json.loads(line.decode("utf-8", errors="replace"))
        if "error" in resp and resp["error"] is not None:
            err = resp["error"]
            raise _RPCError(err.get("message", "rpc error"), denied=err.get("denied", False))
        return resp.get("result")


_VALID_WORKSPACES = ("inherit", "branch", "share")
_VALID_MODELS = (None, "inherit", "flash_lite", "flash", "pro")


def _parse_schema(schema):
    if schema is None:
        raise ValueError("schema is required")
    if isinstance(schema, str):
        try:
            schema = json.loads(schema)
        except json.JSONDecodeError as e:
            raise ValueError(f"invalid schema JSON: {e}") from e
    if not isinstance(schema, dict):
        raise ValueError("schema must be a dict or JSON string")
    return schema


class _AgentTurn:
    def __init__(self, handle, prompt, schema):
        self._handle = handle
        self._prompt = prompt
        self._schema = schema
        self._lock = None
        self._done = False
        self._result = None
        self._error = None

    def __await__(self):
        return self._run().__await__()

    async def _run(self):
        if self._lock is None:
            self._lock = asyncio.Lock()
        async with self._lock:
            if not self._done:
                try:
                    self._result = await self._handle._run_turn(self._prompt, self._schema)
                except BaseException as e:
                    self._error = e
                finally:
                    self._done = True
            if self._error is not None:
                raise self._error
            return self._result


class _AgentHandle:
    def __init__(self, bridge, open_agents, prompt=None, schema=None, role=None,
                 type_name="self", workspace="inherit", model=None):
        if workspace not in _VALID_WORKSPACES:
            raise ValueError(
                f"invalid workspace {workspace!r}: must be one of {_VALID_WORKSPACES}"
            )
        if model not in _VALID_MODELS:
            raise ValueError(
                f"invalid model {model!r}: must be one of {_VALID_MODELS}"
            )
        self._bridge = bridge
        self._open_agents = open_agents
        self._agent_id = f"wf-agent-{uuid.uuid4().hex[:12]}"
        self._prompt = prompt
        self._schema = schema
        self._role = role
        self._type_name = type_name if type_name is not None else "self"
        self._workspace = workspace
        self._model = model

        self._mode = None  # "awaitable" or "callable"
        self._lock = None
        self._turn_lock = None
        self._done = False
        self._result = None
        self._error = None
        self._turn_index = 0
        self._started = False
        self._closed = False

    def __await__(self):
        if self._mode == "callable":
            raise RuntimeError("agent cannot be both awaited and called")
        self._mode = "awaitable"
        return self._run_single().__await__()

    def __call__(self, prompt=None, schema=None):
        if self._mode == "awaitable":
            raise RuntimeError("agent cannot be both awaited and called")
        self._mode = "callable"
        return _AgentTurn(self, prompt, schema)

    async def _run_single(self):
        if self._lock is None:
            self._lock = asyncio.Lock()
        async with self._lock:
            if not self._done:
                try:
                    parsed_schema = _parse_schema(self._schema)
                    req = {
                        "agent_id": self._agent_id,
                        "prompt": self._prompt or "",
                        "schema": parsed_schema,
                        "role": self._role or "",
                        "type_name": self._type_name,
                        "workspace": self._workspace,
                        "model": self._model or "",
                        "turn_index": 0,
                    }
                    self._result = await self._bridge.call_async("agent", req)
                except BaseException as e:
                    self._error = e
                finally:
                    self._done = True
            if self._error is not None:
                raise self._error
            return self._result

    async def _run_turn(self, prompt, schema):
        if self._closed:
            raise RuntimeError("agent has been closed")
        if self._turn_lock is None:
            self._turn_lock = asyncio.Lock()
        async with self._turn_lock:
            if self._closed:
                raise RuntimeError("agent has been closed")
            eff_schema = _parse_schema(schema if schema is not None else self._schema)
            turn_idx = self._turn_index
            self._turn_index += 1
            self._started = True
            self._open_agents[self._agent_id] = self

            if turn_idx == 0 and self._prompt:
                if prompt:
                    eff_prompt = f"{self._prompt}\n\n{prompt}"
                else:
                    eff_prompt = self._prompt
            else:
                eff_prompt = prompt or ""

            req = {
                "agent_id": self._agent_id,
                "prompt": eff_prompt,
                "schema": eff_schema,
                "role": self._role or "",
                "type_name": self._type_name,
                "workspace": self._workspace,
                "model": self._model or "",
                "turn_index": turn_idx,
            }
            return await self._bridge.call_async("agent", req)

    def close_sync(self):
        if self._mode == "callable" and self._started and not self._closed:
            self._closed = True
            self._open_agents.pop(self._agent_id, None)
            try:
                self._bridge.call_sync("agent", {
                    "agent_id": self._agent_id,
                    "close": True,
                }, timeout=5.0)
            except Exception:
                pass

    def __del__(self):
        try:
            self.close_sync()
        except Exception:
            pass


class _CallTool:
    def __init__(self, bridge, name, kwargs):
        self._bridge = bridge
        self._name = name
        self._kwargs = kwargs
        self._lock = None
        self._done = False
        self._result = None
        self._error = None

    def __await__(self):
        return self._run().__await__()

    async def _run(self):
        if self._lock is None:
            self._lock = asyncio.Lock()
        async with self._lock:
            if not self._done:
                try:
                    res = await self._bridge.call_async("call_tool", {
                        "name": self._name,
                        "args": self._kwargs,
                    })
                    self._result = "" if res is None else str(res)
                except _RPCError as e:
                    self._error = ToolError(self._name, e.message, denied=e.denied)
                except BaseException as e:
                    self._error = e
                finally:
                    self._done = True
            if self._error is not None:
                raise self._error
            return self._result


class _Trajectory:
    def __init__(self, bridge, last=None, types=None, tools=None, min_index=None, max_index=None):
        self._bridge = bridge
        self._last = last
        self._types = types
        self._tools = tools
        self._min_index = min_index
        self._max_index = max_index
        self._lock = None
        self._done = False
        self._result = None
        self._error = None

    def __await__(self):
        return self._run().__await__()

    async def _run(self):
        if self._lock is None:
            self._lock = asyncio.Lock()
        async with self._lock:
            if not self._done:
                try:
                    params = {}
                    if self._last is not None:
                        params["last"] = int(self._last)
                    if self._types is not None:
                        params["types"] = [self._types] if isinstance(self._types, str) else list(self._types)
                    if self._tools is not None:
                        params["tools"] = [self._tools] if isinstance(self._tools, str) else list(self._tools)
                    if self._min_index is not None:
                        params["min_index"] = int(self._min_index)
                    if self._max_index is not None:
                        params["max_index"] = int(self._max_index)
                    raw_steps = await self._bridge.call_async("trajectory", params)
                    steps = []
                    for item in (raw_steps or []):
                        if isinstance(item, dict):
                            if item.get("status") == "CLEARED" and "output" not in item:
                                item["output"] = None
                            steps.append(Step(item))
                    self._result = steps
                except BaseException as e:
                    self._error = e
                finally:
                    self._done = True
            if self._error is not None:
                raise self._error
            return self._result


async def _to_coro(aw):
    return await aw


async def parallel(items, fn):
    item_list = list(items)
    if not item_list:
        return []
    tasks = []
    try:
        for item in item_list:
            aw = fn(item)
            if not hasattr(aw, "__await__"):
                raise TypeError(f"parallel fn must return an awaitable, got {type(aw).__name__}")
            tasks.append(asyncio.create_task(_to_coro(aw)))
    except BaseException:
        for t in tasks:
            t.cancel()
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)
        raise

    pending = set(tasks)
    try:
        while pending:
            done, pending = await asyncio.wait(pending, return_when=asyncio.FIRST_EXCEPTION)
            for t in done:
                exc = t.exception()
                if exc is not None:
                    for p in pending:
                        p.cancel()
                    if pending:
                        await asyncio.gather(*pending, return_exceptions=True)
                    raise exc
        return [t.result() for t in tasks]
    except asyncio.CancelledError:
        for t in tasks:
            if not t.done():
                t.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)
        raise


async def pipeline(items, fn):
    results = []
    for item in items:
        aw = fn(item)
        if not hasattr(aw, "__await__"):
            raise TypeError(f"pipeline fn must return an awaitable, got {type(aw).__name__}")
        results.append(await aw)
    return results


class _ReturnRewriter(ast.NodeTransformer):
    def visit_FunctionDef(self, node):
        return node

    def visit_AsyncFunctionDef(self, node):
        return node

    def visit_ClassDef(self, node):
        return node

    def visit_Return(self, node):
        val = node.value if node.value is not None else ast.Constant(value=None)
        assign = ast.Assign(
            targets=[ast.Name(id="__workflow_result__", ctx=ast.Store())],
            value=val,
        )
        ast.copy_location(assign, node)
        ast.fix_missing_locations(assign)
        raise_stmt = ast.Raise(
            exc=ast.Call(
                func=ast.Name(id="_WorkflowReturn", ctx=ast.Load()),
                args=[],
                keywords=[],
            ),
            cause=None,
        )
        ast.copy_location(raise_stmt, node)
        ast.fix_missing_locations(raise_stmt)
        return [assign, raise_stmt]


def _compile_workflow(source, filename):
    tree = compile(source, filename, "exec", ast.PyCF_ONLY_AST | ast.PyCF_ALLOW_TOP_LEVEL_AWAIT)
    tree = _ReturnRewriter().visit(tree)
    ast.fix_missing_locations(tree)
    if tree.body and isinstance(tree.body[-1], ast.Expr):
        last = tree.body[-1]
        assign = ast.Assign(
            targets=[ast.Name(id="__workflow_result__", ctx=ast.Store())],
            value=last.value,
        )
        ast.copy_location(assign, last)
        ast.fix_missing_locations(assign)
        tree.body[-1] = assign
    return compile(tree, filename, "exec", ast.PyCF_ALLOW_TOP_LEVEL_AWAIT)


def _format_script_error(exc, script_name):
    if isinstance(exc, SyntaxError):
        line = exc.lineno or 1
        tb_text = "".join(traceback.format_exception_only(type(exc), exc)).strip()
        msg = f"line {line}: {type(exc).__name__}: {exc.msg}"
        return msg, line, tb_text

    tb = exc.__traceback__
    frames = traceback.extract_tb(tb)
    runtime_file = os.path.abspath(__file__)
    filtered = [f for f in frames if os.path.abspath(f.filename) != runtime_file]
    line = 0
    for f in reversed(filtered):
        if f.filename == script_name:
            line = f.lineno
            break
    if line == 0 and filtered:
        line = filtered[-1].lineno

    lines = ["Traceback (most recent call last):"]
    lines.extend(traceback.format_list(filtered))
    lines.extend(traceback.format_exception_only(type(exc), exc))
    tb_text = "".join(lines).strip()
    if line > 0:
        msg = f"line {line}: {type(exc).__name__}: {exc}"
    else:
        msg = f"{type(exc).__name__}: {exc}"
    return msg, line, tb_text


def _sanitize_sys_path(library_paths):
    runtime_dir = os.path.abspath(os.path.dirname(__file__))
    cwd = os.path.abspath(os.getcwd())
    cleaned = []
    for p in sys.path:
        if p == "":
            continue
        ap = os.path.abspath(p)
        if ap == runtime_dir or ap == cwd:
            continue
        cleaned.append(p)
    for lp in reversed(library_paths or []):
        if lp and lp not in cleaned:
            cleaned.insert(0, lp)
    sys.path[:] = cleaned


async def _run_main(cfg):
    bridge = _BridgeClient(cfg["host"], cfg["port"], cfg["token"])
    open_agents = weakref.WeakValueDictionary()

    def log(*args):
        msg = " ".join(str(a) for a in args)
        bridge.call_sync("log", {"message": msg})

    def phase(name):
        bridge.call_sync("phase", {"name": "" if name is None else str(name)})

    def agent(prompt=None, schema=None, role=None, type_name="self", workspace="inherit", model=None):
        return _AgentHandle(
            bridge,
            open_agents,
            prompt=prompt,
            schema=schema,
            role=role,
            type_name=type_name,
            workspace=workspace,
            model=model,
        )

    def call_tool(name, **kwargs):
        return _CallTool(bridge, name, kwargs)

    def trajectory(last=None, types=None, tools=None, min_index=None, max_index=None):
        return _Trajectory(
            bridge,
            last=last,
            types=types,
            tools=tools,
            min_index=min_index,
            max_index=max_index,
        )

    wf_args = cfg.get("args")
    if not isinstance(wf_args, dict):
        wf_args = {}
    conv_id = cfg.get("conversation_id", "")
    step_index = int(cfg.get("step_index", 0))
    workspaces = tuple(cfg.get("workspaces") or ())

    primitives = {
        "log": log,
        "phase": phase,
        "agent": agent,
        "parallel": parallel,
        "pipeline": pipeline,
        "call_tool": call_tool,
        "trajectory": trajectory,
        "ToolError": ToolError,
        "Step": Step,
        "args": wf_args,
        "CONVERSATION_ID": conv_id,
        "STEP_INDEX": step_index,
        "WORKSPACES": workspaces,
    }
    for k, v in primitives.items():
        setattr(builtins, k, v)

    script_name = cfg.get("script_name") or "<workflow>"
    source = cfg.get("script") or ""
    linecache.cache[script_name] = (
        len(source),
        None,
        source.splitlines(True),
        script_name,
    )

    ns = dict(primitives)
    ns["__name__"] = "__main__"
    ns["__file__"] = script_name
    ns["_WorkflowReturn"] = _WorkflowReturn
    ns["__workflow_result__"] = None

    try:
        code = _compile_workflow(source, script_name)
        try:
            res = eval(code, ns)
            if asyncio.iscoroutine(res):
                await res
        except _WorkflowReturn:
            pass
        result_val = ns.get("__workflow_result__")
        if result_val is not None:
            print(repr(result_val))
        sys.stdout.flush()
        sys.stderr.flush()
        return 0
    except BaseException as exc:
        sys.stdout.flush()
        sys.stderr.flush()
        msg, line, tb_text = _format_script_error(exc, script_name)
        try:
            bridge.call_sync("report_error", {
                "message": msg,
                "line": line,
                "traceback": tb_text,
            }, timeout=5.0)
        except Exception:
            sys.stderr.write(tb_text + "\n")
            sys.stderr.flush()
        return 1
    finally:
        for handle in list(open_agents.values()):
            handle.close_sync()


def main():
    if hasattr(sys.stdout, "reconfigure"):
        sys.stdout.reconfigure(line_buffering=True)
    if hasattr(sys.stderr, "reconfigure"):
        sys.stderr.reconfigure(line_buffering=True)

    cfg_path = sys.argv[1]
    with open(cfg_path, "r", encoding="utf-8") as f:
        cfg = json.load(f)

    script_name = cfg.get("script_name") or "<workflow>"
    sys.argv = [script_name]
    _sanitize_sys_path(cfg.get("library_paths"))

    exit_code = asyncio.run(_run_main(cfg))
    sys.exit(exit_code)


if __name__ == "__main__":
    main()
