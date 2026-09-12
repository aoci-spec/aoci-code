package ui

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPageRepositoryListRecoversFromInvalidResponses(t *testing.T) {
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
// Keep the actual startup, rendering, refresh controls, and polling callback.
source = source.replace(startup, 'globalThis.ui = {loadRepos, state: () => ({repos, current, snap})}; globalThis.started = ' + startup);
const goodList = [{root: '/B', name: 'B', running: 1}, {root: '/A', name: 'A', running: 0}];
const json = value => ({ok: true, status: 200, json: async () => value});
const failures = [
  ['error object', async () => json({error: 'unavailable'})],
  ['HTTP error', async () => ({...json(goodList), ok: false, status: 503})],
  ['network error', async () => { throw new Error('offline'); }],
  ['invalid JSON', async () => ({ok: true, json: async () => { throw new SyntaxError('incomplete JSON'); }})],
  ...[null, 42, 'repos', {length: 1, 0: goodList[0]}, [null], ['repo'], [{}],
      [{root: 42, name: 'B', running: 0}], [{root: '', name: 'B', running: 0}],
      [{root: '/B', running: 0}], [{root: '/B', name: {}, running: 0}],
      [{root: '/B', name: 'B', running: '1'}], [{root: '/B', name: 'B', running: -1}],
      [{root: '/B', name: 'B', running: 0.5}], [goodList[0], null]]
    .map(value => [JSON.stringify(value), async () => json(value)]),
];
function setup(response){
  const elements = new Map(), timers = new Map(), stateRequests = [];
  let nextTimer = 0;
  const get = id => {
    if (!elements.has(id)) elements.set(id, {innerHTML: '', textContent: '', style: {},
      addEventListener(){}, querySelector(){ return null; }, querySelectorAll(){ return []; }});
    return elements.get(id);
  };
  const context = vm.createContext({
    window: {addEventListener(){}, innerHeight: 900},
    document: {documentElement: {}, getElementById: get, querySelector: () => ({offsetHeight: 80})},
    setTimeout: fn => { const id = ++nextTimer; timers.set(id, fn); return id; },
    clearTimeout: id => timers.delete(id),
    fetch: async url => {
      if (url === '/api/repos') return response();
      stateRequests.push(url);
      return {ok: true, status: 200, headers: {get: () => 'state'}, json: async () => ({root: '/B'})};
    },
  });
  vm.runInContext(source, context);
  return {context, get, timers, stateRequests, respond: next => { response = next; },
    poll: async () => {
      assert.equal(timers.size, 1, 'one poll is scheduled');
      const [id, callback] = timers.entries().next().value;
      timers.delete(id);
      await callback();
      assert.equal(timers.size, 1, 'polling continues');
    }};
}
(async () => {
  for (const [label, failure] of failures) {
    const app = setup(failure), {context, get, timers, stateRequests} = app;
    await assert.doesNotReject(context.started, label + ': startup must finish');
    assert.equal(context.ui.state().repos.length, 0, label);
    assert.equal(context.ui.state().current, null, label);
    assert.equal(timers.size, 1, label + ': startup must schedule a poll');
    assert.equal(stateRequests.length, 0, 'no state request without a repository');

    app.respond(async () => json(goodList));
    await app.poll();
    const previous = context.ui.state().repos;
    assert.equal(context.ui.state().current, '/B');
    assert.equal(context.ui.state().snap.root, '/B');
    assert.match(get('repos').innerHTML, /data-root="\/B"/);

    app.respond(failure);
    await app.poll();
    assert.equal(context.ui.state().repos, previous, label + ': preserve the last valid list');
    assert.equal(context.ui.state().current, '/B', label + ': preserve selection');
    assert.equal(stateRequests.length, 2, label + ': continue refreshing the known repository');
    await get('refresh-now').onclick();
    assert.equal(stateRequests.length, 3, label + ': manual refresh still works');
    assert.equal(timers.size, 1);

    app.respond(async () => json([]));
    await context.ui.loadRepos();
    assert.equal(context.ui.state().repos.length, 0, 'a valid empty list is accepted');
    assert.equal(context.ui.state().current, null);
    app.respond(async () => json(goodList));
    await app.poll();
    assert.equal(context.ui.state().current, '/B', 'recovery after an empty list');
  }
})().catch(err => { console.error(err); process.exitCode = 1; });
`)
	cmd.Stdin = strings.NewReader(pageHTML)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("page repository-list regression: %v\n%s", err, output)
	}
}
