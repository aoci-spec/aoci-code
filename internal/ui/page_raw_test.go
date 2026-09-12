package ui

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPageRawFetchRecoversFromErrors(t *testing.T) {
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
source = source.replace(startup, "renderChrome = () => {}; renderAll = () => {}; globalThis.ui = {refresh, rawText, renderRaw, select: (root) => { current = root; asset = 'code'; snap = {assets: {code: {state: 'present'}}}; }};");
function setup(){
  const elements = new Map(), copies = [];
  const get = id => {
    if (!elements.has(id)) elements.set(id, {innerHTML: '', textContent: '', classList: {add(){}, remove(){}}, addEventListener(){}, querySelector(){ return null; }});
    return elements.get(id);
  };
  const context = vm.createContext({
    window: {addEventListener(){}}, document: {documentElement: {}, getElementById: get}, TextEncoder,
    navigator: {clipboard: {writeText: async text => copies.push(text)}}, setTimeout(){},
  });
  vm.runInContext(source, context);
  context.ui.select('repo');
  return {context, ui: context.ui, get, copies};
}
const raw = text => ({ok: true, text: async () => text});
const failures = [
  async () => ({ok: false, status: 500}),
  async () => { throw new Error('network down'); },
  async () => ({ok: true, text: async () => { throw new Error('body interrupted'); }}),
];
(async () => {
  for (const failure of failures) {
    const {context, ui, get, copies} = setup();
    context.fetch = failure;
    await ui.renderRaw();
    assert.match(get('raw-host').innerHTML, /ui.page.disconnected/);
    assert.equal(get('filter-count').textContent, '');
    await get('copy-all').onclick();
    assert.equal(copies.length, 0, 'a failed fetch must not overwrite the clipboard');
    assert.equal(get('copy-all').textContent, 'ui.page.copy_failed');
    let requests = 0;
    context.fetch = async url => {
      if (url.startsWith('/api/state')) return {status: 304};
      requests++;
      return raw('recovered code');
    };
    await ui.refresh(false);
    await new Promise(setImmediate);
    assert.equal(requests, 1, 'unchanged state still retries an uncached asset');
    assert.match(get('raw-host').innerHTML, /recovered code/);
    await get('copy-all').onclick();
    assert.equal(copies[0], 'recovered code');
    await ui.refresh(false);
    assert.equal(requests, 1, 'successful content stays cached');
  }
  // A slow raw retry must not hold up completion of the state poll.
  {
    const {context, ui} = setup(); let resolveRaw, completed = false;
    context.fetch = async url => url.startsWith('/api/state') ? {status: 304} : new Promise(r => { resolveRaw = r; });
    const poll = ui.refresh(false).then(() => { completed = true; });
    await new Promise(setImmediate);
    assert.equal(completed, true, 'raw loading must not delay the next poll');
    resolveRaw(raw('late code')); await poll; await new Promise(setImmediate);
  }
  // Empty successful assets are valid cache entries, unlike failed fetches.
  {
    const {context, ui} = setup(); let requests = 0;
    context.fetch = async () => { requests++; return raw(''); };
    assert.equal(await ui.rawText('code'), '');
    assert.equal(await ui.rawText('code'), '');
    assert.equal(requests, 1);
  }
  // A late failure must not replace another repository's content or copy label.
  for (const operation of ['render', 'copy']) {
    const {context, ui, get} = setup(); let reject;
    context.fetch = () => new Promise((_, r) => { reject = r; });
    const pending = operation === 'render' ? ui.renderRaw() : get('copy-all').onclick();
    ui.select('other'); context.fetch = async () => raw('other code');
    await ui.renderRaw(); get('copy-all').textContent = 'other copy';
    reject(new Error('old request failed')); await pending;
    assert.match(get('raw-host').innerHTML, /other code/);
    assert.equal(get('copy-all').textContent, 'other copy');
  }
})().catch(err => { console.error(err); process.exitCode = 1; });
`)
	cmd.Stdin = strings.NewReader(pageHTML)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("page raw response regression: %v\n%s", err, output)
	}
}
