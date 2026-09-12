package ui

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPageFilterPreservesContextWithLinearWork(t *testing.T) {
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
source = source.replace(startup, "globalThis.render = async (query, tab) => {current = 'repo'; asset = tab; filter = query; rawCache = new Map(); snap = {assets: {code: {state: 'present'}}, chunk_plan: {available: true}}; await renderRaw();};");
const elements = new Map();
const get = id => {
  if (!elements.has(id)) elements.set(id, {innerHTML: '', textContent: '', addEventListener(){}});
  return elements.get(id);
};
let text = '';
const context = vm.createContext({
  window: {addEventListener(){}, AOCI_I18N: {'en-US': {'ui.page.matched_lines': '{a}/{b}', 'ui.page.no_match': 'No match'}}},
  document: {documentElement: {}, getElementById: get}, TextEncoder,
  fetch: async () => ({ok: true, text: async () => text}),
});
vm.runInContext(source, context);
// Count prefix inspections instead of relying on machine-dependent timing.
vm.runInContext('globalThis.prefixChecks = 0; const originalStartsWith = String.prototype.startsWith; String.prototype.startsWith = function(...args) {prefixChecks++; return originalStartsWith.apply(this, args);};', context);
const section = s => '<span class="sec">' + s + '</span>';
const banner = s => '<span class="asset">' + s + '</span>';
const cases = [
  {name: 'sections, case folding, escaping, and a trailing newline', tab: 'code', query: ' NEEDLE ',
    rows: ['#header', '===/first/===', 'x <Needle>&', 'unrelated', '===/second/===', 'needle end'], trailing: '\n', matches: 2,
    visible: [section('===/first/==='), 'x &lt;<mark>Needle</mark>&gt;&amp;', section('===/second/==='), '<mark>needle</mark> end']},
  {name: 'multiple assets', tab: 'all', query: 'hit',
    rows: ['AOCI Cognition Asset: code', '===/code/===', 'hit code', 'skip', 'AOCI Cognition Asset: meta', '===/meta/===', 'hit meta'], matches: 2,
    visible: [banner('AOCI Cognition Asset: code'), section('===/code/==='), '<mark>hit</mark> code', banner('AOCI Cognition Asset: meta'), section('===/meta/==='), '<mark>hit</mark> meta']},
  {name: 'a matching section keeps the preceding section', tab: 'code', query: 'second',
    rows: ['===/first/===', 'ignore', '===/second/===', 'ignore'], matches: 1,
    visible: [section('===/first/==='), section('===/<mark>second</mark>/===')]},
  {name: 'a matching banner keeps the preceding context', tab: 'all', query: 'meta',
    rows: ['AOCI Cognition Asset: code', '===/code/===', 'ignore', 'AOCI Cognition Asset: meta'], matches: 1,
    visible: [banner('AOCI Cognition Asset: code'), section('===/code/==='), banner('AOCI Cognition Asset: <mark>meta</mark>')]},
  {name: 'banner without a section', tab: 'all', query: 'hit', rows: ['AOCI Cognition Asset: meta', '#hit'], matches: 1,
    visible: [banner('AOCI Cognition Asset: meta'), '<span class="cmt">#<mark>hit</mark></span>']},
  {name: 'match before any context', tab: 'code', query: 'hit', rows: ['hit', 'ignore'], matches: 1, visible: ['<mark>hit</mark>']},
  {name: 'no matches', tab: 'code', query: 'absent', rows: ['===/code/===', 'text'], matches: 0, visible: ['No match']},
  {name: 'empty asset', tab: 'code', query: 'hit', rows: [''], matches: 0, visible: ['No match']},
  {name: 'empty filter keeps all lines', tab: 'code', query: ' ', rows: ['#header', '===/code/===', '<text>'],
    visible: ['<span class="cmt">#header</span>', section('===/code/==='), '&lt;text&gt;']},
];
(async () => {
  for (const test of cases) {
    text = test.rows.join('\n') + (test.trailing || '');
    await context.render(test.query, test.tab);
    assert.equal(get('raw-host').innerHTML, '<pre class="raw">' + test.visible.join('\n') + '</pre>', test.name);
    if (test.matches !== undefined) assert.equal(get('filter-count').textContent, test.matches + '/' + test.rows.length, test.name);
  }
  // A standalone Code Volume has sections but no asset banner: every match
  // used to scan all the way back to the beginning looking for that banner.
  for (const count of [1000, 4000]) {
    text = '===/code/===\n' + Array.from({length: count}, (_, i) => 'file' + i + '.go: needle').join('\n');
    context.prefixChecks = 0;
    await context.render('needle', 'code');
    assert.equal(get('raw-host').innerHTML.split('<mark>needle</mark>').length - 1, count);
    assert.ok(context.prefixChecks <= 8 * (count + 1), 'filter work must stay linear: ' + context.prefixChecks + ' prefix checks for ' + count + ' entries');
  }
})().catch(err => {console.error(err); process.exitCode = 1;});
`)
	cmd.Stdin = strings.NewReader(pageHTML)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("page filter regression: %v\n%s", err, output)
	}
}
