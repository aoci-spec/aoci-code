package ui

import (
	"os/exec"
	"strings"
	"testing"
)

// Execute the embedded page itself with controlled fetches and timers. Node is
// available on the CI runner; Go-only development environments can still test
// the server without installing a JavaScript runtime.
func TestPageRefreshRecoversFromResponseErrors(t *testing.T) {
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
source = source.replace(startup, "current = 'repo'; renderChrome = () => {}; renderAll = () => {}; globalThis.ui = {refresh, schedule, state: () => ({snap, etag, lastTick})};");
const element = {addEventListener(){}, querySelector(){ return null; }};
const timers = [];
const context = vm.createContext({
  window: {addEventListener(){}},
  document: {documentElement: {}, getElementById: () => element},
  setTimeout: fn => { timers.push(fn); return timers.length; }, clearTimeout(){},
});
vm.runInContext(source, context);
const good = value => ({ok: true, status: 200, headers: {get: () => value}, json: async () => ({root: value})});
const failures = [
  async () => { throw new Error('network down'); },
  async () => ({ok: false, status: 500, headers: {get: () => 'bad'}, json: async () => ({error: 'server'})}),
  async () => ({ok: true, status: 200, headers: {get: () => 'bad'}, json: async () => { throw new SyntaxError('truncated JSON'); }}),
];
(async () => {
  for (const failure of failures) {
    context.fetch = async () => good('previous');
    await context.ui.refresh(true);
    const previous = context.ui.state().snap;
    context.fetch = url => url === '/api/repos' ? Promise.resolve({json: async () => [{root: 'repo'}]}) : failure();
    context.ui.schedule();
    await timers.pop()();
    assert.equal(context.ui.state().snap, previous, 'keep the last good snapshot');
    assert.equal(context.ui.state().etag, 'previous', 'do not accept an ETag before parsing succeeds');
    assert.equal(context.ui.state().lastTick, 'ui.page.disconnected');
    assert.equal(timers.length, 1, 'schedule another poll after failure');
    context.fetch = async url => url === '/api/repos' ? {json: async () => [{root: 'repo'}]} : good('recovered');
    await timers.pop()();
    assert.equal(context.ui.state().snap.root, 'recovered');
    assert.equal(timers.length, 1);
    timers.pop();
  }
  const previous = context.ui.state().snap;
  context.fetch = async () => ({status: 304});
  await context.ui.refresh(false);
  assert.equal(context.ui.state().snap, previous);
  assert.equal(context.ui.state().lastTick, 'ui.page.up_to_date');
})().catch(err => { console.error(err); process.exitCode = 1; });
`)
	cmd.Stdin = strings.NewReader(pageHTML)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("page refresh regression: %v\n%s", err, output)
	}
}
