package ui

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPageIgnoresOutOfOrderResponses(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for page JavaScript regression tests")
	}
	cmd := exec.Command(node, "-e", `
const assert = require('node:assert/strict');
const vm = require('node:vm');
const html = require('node:fs').readFileSync(0, 'utf8');
let source = html.split('<script>')[1].split('</script>')[0];
const startup = '(async () => { renderChrome(); await loadRepos(); renderChrome(); await refresh(true); schedule(); })();';
assert.ok(source.includes(startup));
source = source.replace(startup, "renderChrome = () => {}; renderAll = () => {}; copyText = async text => copies.push(text); globalThis.ui = {refresh, rawText, renderRaw, state: () => ({snap, etag, lastTick}), select: (root, tab) => { current = root; asset = tab; snap = {assets: {code: {state: 'present'}, meta: {state: 'present'}}}; }};");
function setup(){
  const elements = new Map();
  const get = id => { if (!elements.has(id)) elements.set(id, {innerHTML: '', addEventListener(){}, querySelector(){ return null; }}); return elements.get(id); };
  const context = vm.createContext({window: {addEventListener(){}}, document: {documentElement: {}, getElementById: get}, TextEncoder, copies: []});
  vm.runInContext(source, context);
  context.ui.select('A', 'code');
  return {context, ui: context.ui, get};
}
function deferred(){ let resolve; const promise = new Promise(r => resolve = r); return {promise, resolve}; }
const response = root => ({ok: true, status: 200, headers: {get: () => root}, json: async () => ({root})});
const raw = text => ({ok: true, text: async () => text});
(async () => {
  // An older repository's fetch must not overwrite the selected repository.
  {
    const {context, ui} = setup(), old = deferred();
    context.fetch = () => old.promise;
    const pending = ui.refresh(true);
    ui.select('B', 'code');
    context.fetch = async () => response('B');
    await ui.refresh(true);
    old.resolve(response('A'));
    await pending;
    assert.equal(ui.state().snap.root, 'B');
    assert.equal(ui.state().etag, 'B');
  }
  // Even within one repository, an older JSON body may finish last.
  {
    const {context, ui} = setup(), body = deferred(), parsing = deferred();
    context.fetch = async () => ({ok: true, status: 200, headers: {get: () => 'old'}, json: () => { parsing.resolve(); return body.promise; }});
    const pending = ui.refresh(true);
    await parsing.promise;
    context.fetch = async () => response('new');
    await ui.refresh(true);
    body.resolve({root: 'old'});
    await pending;
    assert.equal(ui.state().snap.root, 'new');
    assert.equal(ui.state().etag, 'new');
  }
  // Changing tabs while an asset loads must not render the previous tab.
  {
    const {context, ui, get} = setup(), old = deferred();
    context.fetch = () => old.promise;
    const pending = ui.renderRaw();
    ui.select('A', 'meta');
    context.fetch = async () => raw('new meta');
    await ui.renderRaw();
    old.resolve(raw('old code'));
    await pending;
    assert.match(get('raw-host').innerHTML, /new meta/);
    assert.doesNotMatch(get('raw-host').innerHTML, /old code/);
  }
  // The same asset name in another repository needs a separate cache key.
  {
    const {context, ui} = setup();
    context.fetch = async () => raw('A code');
    assert.equal(await ui.rawText('code'), 'A code');
    ui.select('B', 'code');
    context.fetch = async () => raw('B code');
    assert.equal(await ui.rawText('code'), 'B code');
  }
  // A pre-refresh raw request cannot repopulate the refreshed cache.
  {
    const {context, ui} = setup(), old = deferred();
    context.fetch = () => old.promise;
    const pending = ui.rawText('code');
    context.fetch = async () => response('A');
    await ui.refresh(true);
    context.fetch = async () => raw('fresh code');
    assert.equal(await ui.rawText('code'), 'fresh code');
    old.resolve(raw('old code'));
    await pending;
    assert.equal(await ui.rawText('code'), 'fresh code');
  }
  // A delayed copy must not copy the previously selected asset.
  {
    const {context, ui, get} = setup(), old = deferred();
    context.fetch = () => old.promise;
    const pending = get('copy-all').onclick();
    ui.select('B', 'meta');
    old.resolve(raw('A code'));
    await pending;
    assert.equal(context.copies.length, 0);
  }
})().catch(err => { console.error(err); process.exitCode = 1; });
`)
	cmd.Stdin = strings.NewReader(pageHTML)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("page response ordering regression: %v\n%s", err, output)
	}
}
