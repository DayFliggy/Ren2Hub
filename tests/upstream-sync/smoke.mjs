import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { createWriteStream, mkdtempSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { once } from "node:events";

if (!process.argv[2])
  throw new Error(
    "Usage: node tests/upstream-sync/smoke.mjs <built-executable>",
  );
const directory = mkdtempSync(join(tmpdir(), "ren2hub-upstream-smoke-"));
const password = randomBytes(24).toString("hex");
const modelName = "gpt-4o-mini";
const sent = [];
const upstream = createServer(async (request, response) => {
  const chunks = [];
  for await (const chunk of request) chunks.push(chunk);
  const body = JSON.parse(Buffer.concat(chunks).toString("utf8"));
  sent.push(request.url);
  response.setHeader("Content-Type", "application/json");
  if (request.url === "/v1/responses") {
    response.end(
      JSON.stringify({
        id: "resp-smoke",
        object: "response",
        status: "completed",
        model: modelName,
        output: [
          {
            type: "custom_tool_call",
            id: "ctc_smoke",
            call_id: "call_smoke",
            name: "exec",
            input: "echo smoke",
          },
        ],
        usage: { input_tokens: 10, output_tokens: 5, total_tokens: 15 },
      }),
    );
    return;
  }
  const toolName = body.tools?.[0]?.function?.name;
  response.end(
    JSON.stringify({
      id: "chat-smoke",
      object: "chat.completion",
      model: modelName,
      choices: [
        {
          index: 0,
          message: toolName
            ? {
                role: "assistant",
                content: null,
                tool_calls: [
                  {
                    id: "call_smoke",
                    type: "function",
                    function: {
                      name: toolName,
                      arguments: '{"city":"Shanghai"}',
                    },
                  },
                ],
              }
            : { role: "assistant", content: "smoke response" },
          finish_reason: toolName ? "tool_calls" : "stop",
        },
      ],
      usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 },
    }),
  );
});
upstream.listen(0, "127.0.0.1");
await once(upstream, "listening");
const upstreamURL = `http://127.0.0.1:${upstream.address().port}`;
const probe = createServer();
probe.listen(0, "127.0.0.1");
await once(probe, "listening");
const port = probe.address().port;
await new Promise((resolve) => probe.close(resolve));
const base = `http://127.0.0.1:${port}`;
const environment = {
  ...process.env,
  PORT: String(port),
  SQL_DSN: "",
  LOG_SQL_DSN: "",
  REDIS_CONN_STRING: "",
  SQLITE_PATH: join(directory, "fresh.db"),
  SESSION_SECRET: randomBytes(32).toString("hex"),
  CRYPTO_SECRET: randomBytes(32).toString("hex"),
  NODE_TYPE: "master",
  DEBUG: "false",
  MEMORY_CACHE_ENABLED: "false",
  NO_PROXY: "127.0.0.1,localhost",
};
const appLog = createWriteStream(join(directory, "application.log"));
const app = spawn(resolve(process.argv[2]), [], {
  cwd: directory,
  env: environment,
  windowsHide: true,
  stdio: ["ignore", "pipe", "pipe"],
});
app.stdout.pipe(appLog);
app.stderr.pipe(appLog);
let spawnError;
app.on("error", (error) => {
  spawnError = error;
});
let bearer = "";

async function call(
  method,
  path,
  body,
  token = bearer,
  proof = "",
  expectedStatus = 200,
  extraHeaders = {},
) {
  const response = await fetch(base + path, {
    method,
    signal: AbortSignal.timeout(15_000),
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(proof ? { "X-Security-Proof": proof } : {}),
      ...extraHeaders,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (response.status !== expectedStatus)
    throw new Error(
      `${method} ${path}: unexpected HTTP ${response.status}, expected ${expectedStatus}`,
    );
  const value = await response.json();
  if (expectedStatus === 200 && value.success === false)
    throw new Error(
      `${method} ${path}: business failure (${value.code || "no code"})`,
    );
  return value;
}

async function proof(scope, context) {
  const result = await call("POST", "/api/verify", {
    scope,
    context,
    method: "password",
    password,
  });
  assert.equal(typeof result.data.proof_token, "string");
  return result.data.proof_token;
}

try {
  let started = false;
  for (let attempt = 0; attempt < 150; attempt++) {
    if (spawnError) throw spawnError;
    if (app.exitCode !== null)
      throw new Error(
        "Application exited before startup; inspect isolated application.log",
      );
    try {
      const response = await fetch(base + "/api/setup", {
        signal: AbortSignal.timeout(500),
      });
      if (response.ok) {
        started = true;
        break;
      }
    } catch {
      /* Only the startup probe retries while the child binds its port. */
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  assert.ok(started, "fresh application must start");
  await call("POST", "/api/setup", {
    username: "smokeadmin",
    password,
    confirmPassword: password,
    SelfUseModeEnabled: false,
    DemoSiteEnabled: false,
  });
  const login = await call("POST", "/api/user/login", {
    username: "smokeadmin",
    password,
  });
  bearer = login.data.access_token;
  assert.equal(typeof bearer, "string");
  const created = await call(
    "POST",
    "/api/user/self/personal-access-tokens",
    { name: "smoke-read", scopes: ["profile:read"] },
    bearer,
    await proof("pat.create"),
  );
  const pat = created.data.token;
  const patID = created.data.personal_access_token.id;
  assert.equal(typeof pat, "string");
  const listed = await call("GET", "/api/user/personal-access-tokens");
  assert.equal(listed.data.length, 1);
  assert.equal(
    JSON.stringify(listed).includes(pat),
    false,
    "PAT plaintext must only appear in its creation response",
  );
  assert.equal(listed.data[0].digest, undefined);
  const scopes = await call(
    "GET",
    "/api/user/self/personal-access-tokens/scopes",
  );
  assert.ok(scopes.data.scopes.includes("ops:read"));
  await call("PATCH", `/api/user/self/personal-access-tokens/${patID}`, {
    name: "smoke-renamed",
  });
  const grants = ["profile:read", "ops:read"];
  await call(
    "PATCH",
    `/api/user/self/personal-access-tokens/${patID}`,
    { scopes: grants },
    bearer,
    await proof("pat.update", { id: patID, scopes: grants }),
  );
  await call("GET", "/api/user/self", undefined, pat);
  await call("GET", "/api/option/", undefined, pat, "", 403);
  await call(
    "GET",
    "/api/user/personal-access-tokens",
    undefined,
    pat,
    "",
    403,
  );

  const card = await call("POST", "/api/models/", {
    model_name: modelName,
    status: 1,
    sync_official: 0,
    capabilities: {
      reasoning_efforts: null,
      input_modalities: ["text"],
      output_modalities: ["text"],
      context_limit: 4096,
      output_limit: 1024,
      tool_call: true,
      source: "isolated smoke fixture",
    },
  });
  assert.equal(card.data.capabilities.tool_call, true);
  const saved = await call("GET", `/api/models/${card.data.id}`);
  assert.equal(saved.data.capabilities.context_limit, 4096);
  const pricing = await call("GET", "/api/option/model_pricing");
  await call("PATCH", "/api/option/model_pricing", {
    changes: [
      {
        model_name: modelName,
        expected_version: pricing.data.empty_version,
        pricing: { ModelPrice: "0.001" },
      },
    ],
  });
  await call("POST", "/api/channel/", {
    mode: "single",
    channel: {
      type: 1,
      name: "isolated-smoke",
      key: "test-only-upstream-key",
      base_url: upstreamURL,
      models: modelName,
      status: 1,
      group: "default",
      channel_ratio: "1",
      capacity_total: 2,
    },
  });
  const codexChannelName = "isolated-codex-cache-only";
  await call("POST", "/api/channel/", {
    mode: "single",
    channel: {
      type: 57,
      name: codexChannelName,
      key: JSON.stringify({
        access_token: "fixture-only",
        account_id: "fixture-account",
      }),
      base_url: upstreamURL,
      models: modelName,
      status: 2,
      group: "default",
      channel_ratio: "1",
      capacity_total: 1,
    },
  });
  const channels = await call("GET", "/api/channel/");
  const codexID = channels.data.items.find(
    (channel) => channel.name === codexChannelName,
  ).id;
  const window = await call("GET", `/api/channel/${codexID}/provider-usage`);
  assert.equal(window.data.state, "unknown");
  assert.equal(window.data.auto_refresh, false);
  assert.deepEqual(window.data.windows, []);
  assert.equal(
    sent.length,
    0,
    "reading an empty usage cache must not call a provider",
  );
  await call("POST", "/api/token/", {
    name: "smoke-relay",
    type: "auto",
    expired_time: -1,
    unlimited_quota: true,
    model_limits_enabled: true,
    model_limits: modelName,
  });
  const tokens = await call("GET", "/api/token/");
  const relayID = tokens.data.items.find(
    (token) => token.name === "smoke-relay",
  ).id;
  const key = await call("POST", `/api/token/${relayID}/key`, {});
  const relayKey = key.data.key;
  assert.equal(typeof relayKey, "string");
  let catalog;
  for (let attempt = 0; attempt < 40; attempt++) {
    catalog = await call("GET", "/v1/models", undefined, relayKey);
    if (catalog.data.some((model) => model.id === modelName)) break;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  assert.deepEqual(
    catalog.data.map((model) => model.id),
    [modelName],
  );
  assert.equal(catalog.data[0].capabilities, undefined);
  assert.equal(catalog.data[0].price_version, undefined);
  const manifest = await call(
    "GET",
    `/api/user/model-manifest?token_id=${relayID}`,
  );
  assert.equal(manifest.data.schema_version, 1);
  assert.deepEqual(
    manifest.data.models.map((model) => model.id),
    [modelName],
  );
  assert.equal(manifest.data.models[0].capabilities.tool_call, true);
  const names = await call("GET", "/api/user/models");
  assert.deepEqual(names.data, [modelName]);
  await call("PUT", "/api/option/", { key: "ServerAddress", value: base });
  for (const format of ["codex", "pi"]) {
    const exported = await call(
      "GET",
      `/api/token/${relayID}/client-config?format=${format}`,
      undefined,
      bearer,
      "",
      200,
      { "X-Forwarded-Host": "untrusted.invalid" },
    );
    assert.deepEqual(exported.data.models, [modelName]);
    assert.ok(exported.data.content.includes(base));
    assert.ok(exported.data.content.includes("REN2HUB_API_KEY"));
    assert.ok(!exported.data.content.includes(relayKey));
    assert.ok(!exported.data.content.includes("untrusted.invalid"));
    if (format === "pi")
      assert.ok(JSON.parse(exported.data.content).providers.ren2hub);
  }
  const chat = await call(
    "POST",
    "/v1/chat/completions",
    {
      model: modelName,
      messages: [{ role: "user", content: "hello" }],
      max_tokens: 20,
    },
    relayKey,
    "",
    200,
    {
      "X-Ren2Hub-Trace-Id": "smoke-trace",
      "X-Ren2Hub-Thread-Id": "smoke-thread",
    },
  );
  assert.equal(chat.choices[0].message.content, "smoke response");
  let diagnostic;
  for (let attempt = 0; attempt < 50; attempt++) {
    const diagnostics = await call(
      "GET",
      `/api/log/self/request-diagnostics?trace_id=smoke-trace&token_id=${relayID}`,
    );
    diagnostic = diagnostics.data.items[0];
    if (diagnostic) break;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  assert.ok(
    diagnostic,
    "bounded observer must persist a safe request projection",
  );
  assert.equal(diagnostic.thread_id, "smoke-thread");
  assert.equal(diagnostic.attempts.length, 1);
  assert.ok(diagnostic.attempts[0].upstream_latency_ms >= 0);
  assert.equal(diagnostic.attempts[0].channel_id, undefined);
  assert.equal(diagnostic.attempts[0].credential_id, undefined);
  const adminProjection = await call(
    "GET",
    `/api/log/requests/${diagnostic.request_id}/diagnostics`,
    undefined,
    pat,
  );
  assert.equal(
    adminProjection.data.attempts[0].channel_id,
    undefined,
    "ops scope alone must not borrow the owner's channel permission",
  );
  assert.equal(adminProjection.data.attempts[0].credential_id, undefined);
  const claude = await call(
    "POST",
    "/v1/messages",
    {
      model: modelName,
      max_tokens: 20,
      messages: [{ role: "user", content: "weather" }],
      tools: [
        {
          name: "weather",
          description: "fixture",
          input_schema: {
            type: "object",
            properties: { city: { type: "string" } },
            required: ["city"],
          },
        },
      ],
    },
    relayKey,
  );
  const tool = claude.content.find((block) => block.type === "tool_use");
  assert.equal(tool.name, "weather");
  assert.equal(tool.input.city, "Shanghai");
  const custom = await call(
    "POST",
    "/v1/responses",
    {
      model: modelName,
      input: "run fixture",
      max_output_tokens: 20,
      tools: [{ type: "custom", name: "exec", description: "fixture" }],
    },
    relayKey,
  );
  assert.equal(custom.output[0].type, "custom_tool_call");
  assert.equal(custom.output[0].input, "echo smoke");
  assert.equal(sent.length, 3);
  // Receiving the HTTP body can precede the handler's final settlement.
  // Wait for the durable wallet projection, without accepting pending funds.
  let owner;
  for (let attempt = 0; attempt < 50; attempt++) {
    owner = await call("GET", "/api/user/self");
    if (owner.data.used_quota === "1500" && owner.data.reserved_quota === "0")
      break;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  assert.equal(
    owner.data.used_quota,
    "1500",
    "three successful per-call quotes settle once each",
  );
  assert.equal(owner.data.reserved_quota, "0");
  await call(
    "DELETE",
    `/api/user/self/personal-access-tokens/${patID}`,
    undefined,
    bearer,
    await proof("pat.revoke", { id: patID }),
  );
  await call("GET", "/api/user/self", undefined, pat, "", 401);
  const old = await fetch(base + "/api/user/token", {
    headers: { Authorization: `Bearer ${bearer}` },
  });
  assert.equal(
    old.status,
    404,
    "retired single-token endpoint must not match user lookup",
  );
  console.log(
    JSON.stringify({
      result: "PASS",
      scenarios: [
        "fresh setup and login",
        "PAT creation/scopes/rename/bound update/redaction/revocation",
        "retired PAT route",
        "capability CRUD",
        "authorized models projection",
        "manifest and trusted client configuration exports",
        "cache-only provider window",
        "request diagnostics and PAT field permissions",
        "Chat",
        "Claude tool",
        "Responses custom tool",
        "single settlement",
      ],
      upstreamCalls: sent.length,
      artifactDirectory: directory,
    }),
  );
} catch (error) {
  console.error(`Isolated smoke artifacts: ${directory}`);
  throw error;
} finally {
  if (app.exitCode === null && !spawnError) {
    app.kill();
    await once(app, "exit");
  }
  appLog.end();
  upstream.closeAllConnections();
  await new Promise((resolve) => upstream.close(resolve));
}
