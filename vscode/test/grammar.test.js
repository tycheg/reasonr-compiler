// A check of the TextMate grammar: all RFC examples (```rr blocks) parse,
// and characteristic lines get the expected scopes.
"use strict";
const fs = require("fs");
const path = require("path");
const assert = require("assert");
const oniguruma = require("vscode-oniguruma");
const vsctm = require("vscode-textmate");

async function loadGrammar() {
  const wasm = fs.readFileSync(require.resolve("vscode-oniguruma/release/onig.wasm")).buffer;
  await oniguruma.loadWASM(wasm);
  const registry = new vsctm.Registry({
    onigLib: Promise.resolve({
      createOnigScanner: (sources) => new oniguruma.OnigScanner(sources),
      createOnigString: (s) => new oniguruma.OnigString(s),
    }),
    loadGrammar: async () => {
      const file = path.join(__dirname, "..", "syntaxes", "reasonr.tmLanguage.json");
      return vsctm.parseRawGrammar(fs.readFileSync(file, "utf8"), file);
    },
  });
  return registry.loadGrammar("source.reasonr");
}

// tokenize returns for every line a list of [text, scopes].
function tokenize(grammar, text) {
  let state = vsctm.INITIAL;
  return text.split("\n").map((line) => {
    const r = grammar.tokenizeLine(line, state);
    state = r.ruleStack;
    return r.tokens.map((t) => [line.slice(t.startIndex, t.endIndex), t.scopes]);
  });
}

// scopeOf returns the innermost scope of fragment word in the first line
// that starts with prefix.
function scopeOf(lines, source, prefix, word) {
  const i = source.findIndex((l) => l.startsWith(prefix));
  for (const [text, scopes] of lines[i] || []) {
    if (text === word) {
      return scopes[scopes.length - 1];
    }
  }
  return undefined;
}

// rfcBlocks are the ```rr blocks of the RFC-0001 chapters in dir:
// ["chapter:line", text].
function rfcBlocks(dir) {
  const out = [];
  for (const name of fs.readdirSync(dir).filter((n) => n.endsWith(".md")).sort()) {
    out.push(...chapterBlocks(path.join(dir, name), name));
  }
  return out;
}

function chapterBlocks(file, name) {
  const out = [];
  let cur = null;
  fs.readFileSync(file, "utf8").split("\n").forEach((line, i) => {
    if (cur === null && line === "```rr") cur = { line: i + 2, text: [] };
    else if (cur !== null && line === "```") {
      out.push([`${name}:${cur.line}`, cur.text.join("\n") + "\n"]);
      cur = null;
    } else if (cur !== null) cur.text.push(line);
  });
  return out;
}

async function main() {
  const grammar = await loadGrammar();

  // All examples parse, and statement keywords are not taken for fields.
  const rfc = path.join(__dirname, "..", "..", "docs", "rfc", "0001-reasonr-language");
  const blocks = rfcBlocks(rfc);
  assert.ok(blocks.length > 0, "no examples");
  for (const [file, text] of blocks) {
    for (const toks of tokenize(grammar, text)) {
      for (const [text, scopes] of toks) {
        if (/^(let|if|for|return|where|select|language|platforms|default|binds|part|children|in)$/.test(text)) {
        assert.ok(!scopes.some((s) => s.startsWith("variable.other.property")), `${file}: "${text}" is a field`);
        }
      }
    }
  }

  const sample = [
    "## A customer order",
    "entity Order",
    "  id uuid",
    "  status OrderStatus",
    "  phone string? personal",
    "",
    "  command(customer) Cancel(reason string)",
    "    let note = \"reason: {reason.trim()}\"",
    "    if status == OrderStatus.cancelled and after.total > 10.50",
    "      alreadyCancelled()",
    "    cancelled(why: note)   # event",
    "    schedule(at: now + 30m, run: remind())",
    "",
    "  command remind()",
    "    return",
    "",
    "  event cancelled(why string)",
    "    status = OrderStatus.cancelled",
    "",
    "  error alreadyCancelled",
    "",
    "fake Matching",
    "external(matching) Payments",
    "app(Staff, admin) Admin \"Admin\"",
    "  language de",
    "  platforms web, mobile",
    "  default Customer.Id EntityPicker(search: Customer.FindByPhone(phoneNumber: text))",
    "  Menu",
    "screen MyOrder(orderId Order.Id) \"Order\"",
    "  Card Order.Card",
    "    Actions",
    "      Confirm Order.Cancel if status in [searching]",
    "    on open MyOrders",
    "primitive Table<Row>(size int = 20)",
    "  binds page<Row>",
    "  part Filters binds params",
    "  children binds Row",
    "extend screen MyOrder",
    "  in Actions",
    "texts de",
    "  Order.Cancel \"Cancel\"",
  ].join("\n");
  const lines = tokenize(grammar, sample);
  // [line start, fragment, scope]
  const expect = [
    ["##", "## A customer order", "comment.line.documentation.reasonr"],
    ["entity", "entity", "storage.type.reasonr"],
    ["entity", "Order", "entity.name.type.reasonr"],
    ["  id", "id", "storage.type.id.reasonr"],
    ["  id", "uuid", "support.type.primitive.reasonr"],
    ["  status", "status", "variable.other.property.reasonr"],
    ["  status", "OrderStatus", "entity.name.type.reasonr"],
    ["  phone", "personal", "keyword.other.reasonr"],
    ["  command(customer)", "Cancel", "entity.name.function.command.public.reasonr"],
    ["  command(customer)", "customer", "variable.other.reasonr"],
    ["  command remind", "remind", "entity.name.function.command.reasonr"],
    ["  event", "cancelled", "entity.name.function.event.reasonr"],
    ["  error", "alreadyCancelled", "entity.name.type.error.reasonr"],
    ["    let", "let", "keyword.control.reasonr"],
    ["    let", "{", "punctuation.section.interpolation.begin.reasonr"],
    ["    if", "and", "keyword.operator.word.reasonr"],
    ["    if", "10.50", "constant.numeric.decimal.reasonr"],
    ["    if", "after", "variable.language.reasonr"],
    ["    cancelled", "why", "variable.parameter.reasonr"],
    ["    cancelled", "cancelled", "entity.name.function.call.reasonr"],
    ["    cancelled", "# event", "comment.line.number-sign.reasonr"],
    ["    schedule", "schedule", "support.function.builtin.reasonr"],
    ["    schedule", "30m", "constant.numeric.duration.reasonr"],
    ["    schedule", "now", "variable.language.reasonr"],
    ["fake", "fake", "storage.type.reasonr"],
    ["fake", "Matching", "entity.name.type.reasonr"],
    ["external", "external", "storage.type.reasonr"],
    ["external", "matching", "variable.other.reasonr"],
    ["external", "Payments", "entity.name.type.reasonr"],
    ["app", "app", "storage.type.reasonr"],
    ["app", "Admin", "entity.name.type.reasonr"],
    ["app", "admin", "variable.other.reasonr"],
    ["  language", "language", "keyword.other.reasonr"],
    ["  platforms", "platforms", "keyword.other.reasonr"],
    ["  default", "default", "keyword.other.reasonr"],
    ["  Menu", "Menu", "entity.name.tag.primitive.reasonr"],
    ["screen", "screen", "storage.type.reasonr"],
    ["screen", "MyOrder", "entity.name.type.screen.reasonr"],
    ["  Card", "Card", "entity.name.tag.primitive.reasonr"],
    ["    Actions", "Actions", "entity.name.tag.primitive.reasonr"],
    ["      Confirm", "Confirm", "entity.name.tag.primitive.reasonr"],
    ["      Confirm", "if", "keyword.control.reasonr"],
    ["    on open", "on", "keyword.control.on.reasonr"],
    ["primitive", "primitive", "storage.type.reasonr"],
    ["primitive", "Table", "entity.name.type.primitive.reasonr"],
    ["  binds", "binds", "keyword.other.reasonr"],
    ["  part", "part", "storage.type.reasonr"],
    ["  part", "Filters", "entity.name.tag.part.reasonr"],
    ["  part", "binds", "keyword.other.reasonr"],
    ["  children", "children", "keyword.other.reasonr"],
    ["extend", "screen", "keyword.other.extend.reasonr"],
    ["  in", "in", "keyword.control.reasonr"],
    ["texts", "texts", "storage.type.reasonr"],
    ["  Order.Cancel", "Order", "entity.name.type.reasonr"],
  ];
  const source = sample.split("\n");
  const errors = [];
  for (const [prefix, word, want] of expect) {
    const got = scopeOf(lines, source, prefix, word);
    if (got !== want) errors.push(`"${word}": ${got}, expected ${want}`);
  }
  if (errors.length) {
    console.error(errors.join("\n"));
    process.exit(1);
  }
  console.log(`grammar: ${blocks.length} RFC blocks, ${expect.length} checks — ok`);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
