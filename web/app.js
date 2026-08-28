
let DATA=[], PLANS={};
const api=(p,o)=>fetch(p,o).then(async r=>{
  const ct=r.headers.get("content-type")||"";
  const d=ct.includes("json")?await r.json():await r.text();
  if(!r.ok)throw Object.assign(new Error((d&&d.error)||d||r.statusText),{status:r.status,data:d});
  return d});
const STATES=[["","Unrefined"],["plan-review","Plan review"],["ready","Ready"],["triage","Triage"],["done","Done"]];
const GATES=new Set(["plan-review","triage"]);
const PALETTES={
 quiet:{name:"Quiet",light:{},dark:{}},
 slate:{name:"Slate",
  light:{"--ground":"#f4f6f7","--surface":"#ffffff","--surface-2":"#e8edf0","--ink":"#14202a",
   "--ink-muted":"#55677a","--ink-faint":"#8496a6","--hairline":"#dce3e8","--you-bg":"#0f4c5c",
   "--you-fg":"#f4f6f7","--you-soft":"#e6f0f2","--you-line":"#a8c8d1","--claimed":"#3e6e8e",
   "--claimed-soft":"#e6eef3","--done":"#4b7b58","--done-soft":"#e9f1eb",
   "--mark":"#e9f3f6","--mark-line":"#a8c8d1"},
  dark:{"--ground":"#0f1720","--surface":"#16212c","--surface-2":"#1d2a36","--ink":"#e4ecf2",
   "--ink-muted":"#92a6b8","--ink-faint":"#6a7e90","--hairline":"#24313d","--you-bg":"#5fd0e8",
   "--you-fg":"#0f1720","--you-soft":"#17272e","--you-line":"#2c4650","--claimed":"#79aecd",
   "--claimed-soft":"#16242e","--done":"#7fb08c","--done-soft":"#152119",
   "--mark":"#132430","--mark-line":"#2c4650"}},
 term:{name:"Terminal",
  light:{"--ground":"#f2f3f0","--surface":"#ffffff","--surface-2":"#e7eae5","--ink":"#161a16",
   "--ink-muted":"#576057","--ink-faint":"#858d85","--hairline":"#dfe3de","--you-bg":"#1f6b33",
   "--you-fg":"#f2f3f0","--you-soft":"#e7f2e9","--you-line":"#a8cdb2","--claimed":"#3d6b7d",
   "--claimed-soft":"#e6eef1","--done":"#4a7a55","--done-soft":"#e8f1ea",
   "--mark":"#eaf4ec","--mark-line":"#a8cdb2"},
  dark:{"--ground":"#0b0d0c","--surface":"#121512","--surface-2":"#1a1f1a","--ink":"#d6e0d2",
   "--ink-muted":"#8a968a","--ink-faint":"#5c665c","--hairline":"#1e241e","--you-bg":"#7bd88f",
   "--you-fg":"#0b0d0c","--you-soft":"#141c15","--you-line":"#2c3d2f","--claimed":"#6a9fb5",
   "--claimed-soft":"#111a1e","--done":"#5c7a62","--done-soft":"#121a14",
   "--mark":"#121b13","--mark-line":"#2c3d2f"}}};

let pal="quiet",fRepo="all",fState="open",q="",openPlan=null,mode="ren",composing=null;
let comments={},reviews={};
try{const s=JSON.parse(localStorage.getItem("hb3")||"{}");
 pal=s.p||pal;fRepo=s.r||fRepo;fState=s.s||fState;comments=s.c||{};reviews=s.v||{};}catch(e){}
const save=()=>{try{localStorage.setItem("hb3",JSON.stringify({p:pal,r:fRepo,s:fState,c:comments,v:reviews}))}catch(e){}};
const esc=s=>String(s).replace(/[&<>"]/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;"}[c]));
const all=()=>DATA.flatMap(r=>r.tasks.map(t=>({...t,repo:r.repo})));
const isOpen=t=>!t.done&&!t.deferred;
const label=s=>(STATES.find(x=>x[0]===s)||["","Unrefined"])[1];
const find=id=>all().find(t=>t.id===id);
const repoOf=id=>(find(id)||{}).repo;
const cs=id=>comments[id]||[];

function isDark(){const a=document.documentElement.getAttribute("data-theme");
  if(a==="dark")return true;if(a==="light")return false;
  return matchMedia("(prefers-color-scheme: dark)").matches}
const VARS=["--ground","--surface","--surface-2","--ink","--ink-muted","--ink-faint","--hairline",
 "--you-bg","--you-fg","--you-soft","--you-line","--claimed","--claimed-soft","--done","--done-soft","--mark","--mark-line"];
function applyPal(){const s=document.documentElement.style;VARS.forEach(k=>s.removeProperty(k));
  const p=PALETTES[pal][isDark()?"dark":"light"];for(const k in p)s.setProperty(k,p[k])}
matchMedia("(prefers-color-scheme: dark)").addEventListener("change",applyPal);
new MutationObserver(applyPal).observe(document.documentElement,{attributes:true,attributeFilter:["data-theme"]});

function inl(t){return esc(t).replace(/`([^`]+)`/g,"<code>$1</code>")
  .replace(/\*\*([^*]+)\*\*/g,"<strong>$1</strong>")}

/* Markdown -> blocks that remember which source lines they came from, so a
   comment left while reading rendered prose still resolves to a real line for
   the agent. One comment model, two ways to look at it. */
function mdBlocks(src){
  const L=src.split("\n"),out=[];let i=0;
  const push=(a,b,h)=>out.push({start:a+1,end:b+1,html:h});
  while(i<L.length){
    const t=L[i];
    if(!t.trim()){i++;continue}
    if(t.trim().startsWith("```")){
      const a=i;i++;const code=[];
      while(i<L.length&&!L[i].trim().startsWith("```")){code.push(L[i]);i++}
      if(i<L.length)i++;
      push(a,i-1,"<pre><code>"+esc(code.join("\n"))+"</code></pre>");continue}
    const h=t.match(/^(#{1,4})\s+(.*)$/);
    if(h){push(i,i,`<h${h[1].length}>${inl(h[2])}</h${h[1].length}>`);i++;continue}
    if(/^\s*(---+|===+)\s*$/.test(t)){push(i,i,"<hr>");i++;continue}
    if(/^\s*[-*]\s+/.test(t)||/^\s*\d+\.\s+/.test(t)){
      const a=i,ol=/^\s*\d+\.\s+/.test(t),items=[];
      while(i<L.length&&(/^\s*[-*]\s+/.test(L[i])||/^\s*\d+\.\s+/.test(L[i])||
            (L[i].trim()&&/^\s{2,}/.test(L[i])&&items.length))){
        if(/^\s*[-*]\s+/.test(L[i])||/^\s*\d+\.\s+/.test(L[i]))
          items.push(inl(L[i].replace(/^\s*(?:[-*]|\d+\.)\s+/,"")));
        else items[items.length-1]+=" "+inl(L[i].trim());
        i++}
      push(a,i-1,`<${ol?"ol":"ul"}>`+items.map(x=>`<li>${x}</li>`).join("")+`</${ol?"ol":"ul"}>`);continue}
    if(/^\s*>/.test(t)){
      const a=i,q=[];
      while(i<L.length&&/^\s*>/.test(L[i])){q.push(inl(L[i].replace(/^\s*>\s?/,"")));i++}
      push(a,i-1,"<blockquote>"+q.join(" ")+"</blockquote>");continue}
    const a=i,par=[];
    while(i<L.length&&L[i].trim()&&!/^(#{1,4})\s/.test(L[i])&&!L[i].trim().startsWith("```")
          &&!/^\s*[-*]\s+/.test(L[i])&&!/^\s*\d+\.\s+/.test(L[i])&&!/^\s*>/.test(L[i])){
      par.push(inl(L[i]));i++}
    push(a,i-1,"<p>"+par.join(" ")+"</p>");
  }
  return out}

function renderTotals(){const a=all();
  document.getElementById("totals").textContent=`${a.length} tasks · ${a.filter(isOpen).length} open · ${DATA.length} repos`;
  document.getElementById("q").placeholder=`Search ${a.length} tasks…`}
function renderPal(){document.getElementById("pal").innerHTML=Object.entries(PALETTES)
  .map(([k,v])=>`<button class="chip" aria-pressed="${pal===k}" data-pal="${k}">${v.name}</button>`).join("")}
function renderGates(){
  const g=all().filter(t=>isOpen(t)&&GATES.has(t.state)&&!reviews[t.id]);
  document.getElementById("gcount").textContent=g.length;
  const el=document.getElementById("gatelist");
  if(!g.length){el.innerHTML='<div class="empty">Nothing needs you right now.</div>';return}
  el.innerHTML=g.map(t=>{const p=t.hasPlan?{lines:t.planLines||0}:null,n=cs(t.id).length;
    return `<article class="gate">
      <div class="why">${t.state==="plan-review"?"A plan wants your review":"A build wants your eyes"}</div>
      <h3>${esc(t.subject)}</h3>
      <div class="meta"><span class="pill">${esc(t.repo)}</span><span class="id mono">${esc(t.id)}</span>
      ${p?"":"<span>no plan on file</span>"}
      ${n?`<span class="pill you">${n} comment${n>1?"s":""}</span>`:""}</div>
      <div class="acts">${(t.state==="triage"?t.hasBuild:t.hasPlan)
        ? `<button class="primary" data-read="${esc(t.id)}">${n?"Continue review":(t.state==="triage"?"Review the build":"Review the plan")}</button>`
        : `<button disabled>${t.state==="triage"?"No branch found for this ticket":"No plan to read"}</button>`}</div></article>`}).join("")}
function renderPipe(){const a=all();
  document.getElementById("pipe").innerHTML=STATES.map(([s,l],i)=>{
    const n=s==="done"?a.filter(t=>t.done).length:a.filter(t=>isOpen(t)&&t.state===s).length;
    return `<div class="stage${GATES.has(s)&&n?" hot":""}"><div class="n mono">${i+1}</div>
      <div class="v mono">${n}</div><div class="l">${l}</div></div>`}).join("")}
function renderChips(){
  document.getElementById("repochips").innerHTML=[["all","All repos"]].concat(DATA.map(r=>[r.repo,r.repo]))
    .map(([v,l])=>`<button class="chip" aria-pressed="${fRepo===v}" data-repo="${esc(v)}">${esc(l)}</button>`).join("");
  document.getElementById("statechips").innerHTML=
    [["open","Open"],["mine","Waiting on you"],["claimed","Claimed"],["all","Everything"]]
    .map(([v,l])=>`<button class="chip" aria-pressed="${fState===v}" data-state="${v}">${l}</button>`).join("")}
function match(t){
  if(fRepo!=="all"&&t.repo!==fRepo)return false;
  if(fState==="open"&&!isOpen(t))return false;
  if(fState==="mine"&&!(isOpen(t)&&GATES.has(t.state)))return false;
  if(fState==="claimed"&&!t.claim)return false;
  if(q&&!(t.subject+" "+t.desc+" "+t.id).toLowerCase().includes(q))return false;
  return true}
function renderRepos(){
  document.getElementById("repos").innerHTML=DATA.map(r=>{
    const ts=r.tasks.map(t=>({...t,repo:r.repo})).filter(match);
    if(!ts.length)return"";
    const secs={};ts.forEach(t=>{(secs[t.section]=secs[t.section]||[]).push(t)});
    return `<section class="repo"><div class="rh"><h2>${esc(r.repo)}</h2>
      <span class="c mono">${ts.length} shown · ${r.tasks.filter(isOpen).length} open</span></div>
      ${Object.entries(secs).map(([sec,list])=>
        `${Object.keys(secs).length>1?`<div class="sec">${esc(sec)} <span class="mono">${list.length}</span></div>`:""}
        ${list.map(t=>`<div class="task${t.done?" is-done":""}" tabindex="0" role="button" data-open="${esc(t.id)}">
          <span class="box${t.done?" done":""}${t.deferred?" def":""}"></span><span class="t-main">
          <span class="t-sub">${esc(t.subject)}</span><span class="t-meta">
          <span class="id mono">${esc(t.id)}</span>
          ${t.state?`<span class="pill${GATES.has(t.state)?" you":""}">${esc(label(t.state))}</span>`:""}
          ${t.claim?`<span class="pill claimed">@${esc(t.claim)}</span>`:""}
          ${reviews[t.id]?`<span class="pill">${esc(reviews[t.id].verdict)}</span>`:""}
          </span></span></div>
        <div class="body" id="b-${esc(t.id)}">${esc(t.desc)||"<em>No description.</em>"}
          ${t.hasPlan?`<div class="acts"><button class="primary" data-read="${esc(t.id)}">Review the plan</button></div>`:""}
        </div>`).join("")}`).join("")}</section>`}).join("")||'<div class="empty">Nothing matches.</div>'}
function draw(){renderTotals();renderPal();renderGates();renderPipe();renderChips();renderRepos();applyPal()}

/* ---- reviewer ---- */
function lineClass(t){
  if(!t.trim())return"blank";
  if(/^#\s/.test(t))return"h1"; if(/^##\s/.test(t))return"h2"; return""}
function diffClass(t){
  if(/^diff --git |^index |^--- |^\+\+\+ /.test(t))return"dmeta";
  if(/^@@/.test(t))return"dhunk";
  if(/^\+/.test(t))return"dadd";
  if(/^-/.test(t))return"ddel";
  return""}
function renderDiff(){
  const p=PLANS[openPlan],lines=p.text.split("\n"),byLine={};
  cs(openPlan).forEach((c,i)=>{(byLine[c.line]=byLine[c.line]||[]).push({...c,i})});
  let h=`<div class="src diff">`;
  lines.forEach((t,i)=>{
    const n=i+1,has=byLine[n];
    h+=`<div class="ln ${diffClass(t)}${has?" has":""}" id="L${n}">
      <span class="num mono" data-line="${n}" role="button" tabindex="0" title="Comment on line ${n}">${n}</span>
      <span class="txt">${esc(t)||" "}</span></div>`;
    (has||[]).forEach(c=>{h+=threadHtml(c)});
    if(composing===n)h+=composerHtml(n);
  });
  return h+"</div>"}
function renderSource(){
  const p=PLANS[openPlan],lines=p.text.split("\n"),byLine={};
  cs(openPlan).forEach((c,i)=>{(byLine[c.line]=byLine[c.line]||[]).push({...c,i})});
  let h='<div class="src">';
  lines.forEach((t,i)=>{
    const n=i+1,has=byLine[n];
    h+=`<div class="ln ${lineClass(t)}${has?" has":""}" id="L${n}">
      <span class="num mono" data-line="${n}" role="button" tabindex="0" title="Comment on line ${n}">${n}</span>
      <span class="txt">${esc(t)||" "}</span></div>`;
    (has||[]).forEach(c=>{h+=threadHtml(c)});
    if(composing===n)h+=composerHtml(n);
  });
  return h+"</div>"}
function threadHtml(c){return `<div class="thread"><div class="w">Line ${c.line} &middot; you</div>${esc(c.text)}
  <div class="acts"><button class="sm" data-del="${c.i}">Delete</button></div></div>`}
function composerHtml(n){return `<div class="composer"><textarea id="cbox" placeholder="What is wrong here? (line ${n})"></textarea>
  <div class="acts"><button class="primary sm" data-add="${n}">Add comment</button>
  <button class="sm" data-cancel="1">Cancel</button></div></div>`}

/* Rendered prose, still commentable: each block carries its source line range,
   so a comment made here lands on a real line number the agent can act on. */
function renderRendered(){
  const blocks=mdBlocks(PLANS[openPlan].text);
  const list=cs(openPlan).map((c,i)=>({...c,i}));
  let h='<div class="rendered">';
  blocks.forEach(b=>{
    const mine=list.filter(c=>c.line>=b.start&&c.line<=b.end);
    const composeHere=composing!==null&&composing>=b.start&&composing<=b.end;
    h+=`<div class="blk${mine.length?" has":""}" id="B${b.start}">
      <button class="cbtn" data-line="${b.start}" title="Comment on line ${b.start}" aria-label="Comment on line ${b.start}">+</button>
      <span class="lno">${b.start}</span>
      <div class="bhtml">${b.html}</div>
      ${mine.map(threadHtml).join("")}
      ${composeHere?composerHtml(composing):""}</div>`});
  return h+"</div>"}

function renderReader(){
  const isBuild=PLANS[openPlan]&&PLANS[openPlan].kind==="build";
  // A diff has no markdown to render, so the toggle is meaningless for a build.
  document.getElementById("mRen").style.display=isBuild?"none":"";
  document.getElementById("mSrc").style.display=isBuild?"none":"";
  document.getElementById("rmain").innerHTML =
    isBuild ? renderDiff() : (mode==="src" ? renderSource() : renderRendered());
  document.getElementById("mSrc").setAttribute("aria-pressed",mode==="src");
  document.getElementById("mRen").setAttribute("aria-pressed",mode==="ren");
  const n=cs(openPlan).length;
  const isB=PLANS[openPlan]&&PLANS[openPlan].kind==="build";
  // A plan you hold corrections against is not a plan you approve. A build is
  // the other way round: notes on work you are accepting are the ordinary
  // outcome of a triage pass, so they ride along with the acceptance.
  const ap=document.getElementById("vApprove");
  ap.disabled=n>0&&!isB;
  ap.title=ap.disabled?"Delete your comments first, or send it back":"";
  document.getElementById("ccount").textContent = n
    ? (isB
        ? `${n} comment${n>1?"s":""} — recorded with the build when you accept it`
        : `${n} comment${n>1?"s":""} — approval is off until they are resolved`)
    : "No comments — tap a line number to add one";
  ap.textContent=isB?"Accept the build":"Approve";
  renderOutbox();
  if(composing){const b=document.getElementById("cbox");if(b)b.focus()}}
function reviewDoc(verdict){
  const t=find(openPlan),p=PLANS[openPlan],lines=p.text.split("\n"),list=cs(openPlan).slice().sort((a,b)=>a.line-b.line);
  let s=`# Review — ${t.subject}\n\nticket: ${t.id}\nplan: docs/plans/${t.id}.md\nplan-hash: ${p.hash}\n`;
  s+=`reviewer: Steve\nverdict: ${verdict==="approve"?"approved":"changes requested"}\n`;
  s+=`comments: ${list.length}\n\n`;
  if(!list.length)s+="_No line comments._\n";
  list.forEach(c=>{s+=`## Line ${c.line}\n\n> ${(lines[c.line-1]||"").trim()||"(blank line)"}\n\n${c.text}\n\n`});
  return s}
async function submitReview(verdict){
  const t=find(openPlan),p=PLANS[openPlan];
  const body={verdict,kind:p.kind||"plan",hash:p.hash,
    comments:cs(openPlan).map(c=>({line:c.line,text:c.text}))};
  try{
    const res=await api(`/api/review/${encodeURIComponent(t.repo)}/${encodeURIComponent(openPlan)}`,
      {method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(body)});
    reviews[openPlan]={verdict:verdict==="approve"?"Approved":"Changes requested",
      at:new Date().toISOString().slice(0,16).replace("T"," "),hash:p.hash,n:cs(openPlan).length,
      wrote:res.wrote};
    delete comments[openPlan];save();
    await load();closeReader();
    alert((verdict==="approve"?(p.kind==="build"?"Accepted":"Approved"):"Sent back")+
      " — review written to "+res.wrote);
  }catch(e){
    if(e.status===409){
      alert("The "+(p.kind||"plan")+" changed while you were reviewing it.\n\nYour comments point at lines that may have moved, so the review was not recorded. Reopen it to see the new version.");
      delete PLANS[openPlan];closeReader();await load();return}
    alert("Review not saved: "+e.message)}}

async function taskOp(id,body){
  const t=find(id);if(!t)return;
  try{await api(`/api/task/${encodeURIComponent(t.repo)}/${encodeURIComponent(id)}`,
    {method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(body)});
    await load()}catch(e){alert("Not saved: "+e.message)}}

async function load(){
  try{DATA=await api("/api/backlog")}catch(e){return}
  draw()}

function renderOutbox(){
  const el=document.getElementById("outbox");
  const r=openPlan?reviews[openPlan]:null;
  if(!r){el.innerHTML="";return}
  el.innerHTML=`<h4>Recorded</h4><pre>${esc(r.verdict)} · ${esc(r.at)} · against plan-hash ${esc(r.hash)}
written to ${esc(r.wrote||"")}</pre>`}
const kindOf=t=>t.state==="triage"?"build":"plan";
async function openReader(id){
  const t=find(id);if(!t)return;
  const kind=kindOf(t);
  if(kind==="build"&&!t.hasBuild){alert("No unmerged branch carries a commit for this ticket.");return}
  if(kind==="plan"&&!t.hasPlan){alert("No plan on file for this ticket.");return}
  if(!PLANS[id]){
    try{PLANS[id]=await api(`/api/${kind}/${encodeURIComponent(t.repo)}/${encodeURIComponent(id)}`)}
    catch(e){alert("Could not load the "+kind+": "+e.message);return}}
  const p=PLANS[id];p.kind=kind;
  openPlan=id;composing=null;mode="ren";
  document.getElementById("rtitle").textContent=t.subject;
  document.getElementById("rhash").textContent =
    `${t.id} · ${p.path||""} · ${p.lines} lines · ${p.hash}`;
  document.getElementById("reader").classList.add("open");
  document.body.style.overflow="hidden";
  renderReader();document.getElementById("reader").scrollTop=0}
function closeReader(){openPlan=null;composing=null;
  document.getElementById("reader").classList.remove("open");
  document.body.style.overflow="";draw()}

document.addEventListener("click",e=>{
  const P=e.target.closest("[data-pal]");if(P){pal=P.dataset.pal;save();applyPal();renderPal();return}
  const R=e.target.closest("[data-read]");if(R){openReader(R.dataset.read);return}
  if(e.target.closest("#rclose")){closeReader();return}
  if(e.target.closest("#mSrc")){mode="src";renderReader();return}
  if(e.target.closest("#mRen")){mode="ren";renderReader();return}
  const L=e.target.closest("[data-line]");
  if(L){const n=+L.dataset.line;composing=composing===n?null:n;renderReader();
    const el=document.getElementById((mode==="src"?"L":"B")+n);
    if(el)el.scrollIntoView({block:"center"});return}
  const A=e.target.closest("[data-add]");
  if(A){const v=(document.getElementById("cbox").value||"").trim();
    if(!v){alert("Write the correction first.");return}
    (comments[openPlan]=comments[openPlan]||[]).push({line:+A.dataset.add,text:v});
    composing=null;save();renderReader();return}
  if(e.target.closest("[data-cancel]")){composing=null;renderReader();return}
  const D=e.target.closest("[data-del]");
  if(D){comments[openPlan].splice(+D.dataset.del,1);save();renderReader();return}
  const V=e.target.closest("[data-verdict]");
  if(V){const n=cs(openPlan).length,vb=PLANS[openPlan]&&PLANS[openPlan].kind==="build";
    if(V.dataset.verdict==="approve"&&n&&!vb){
      alert("You have "+n+" comment"+(n>1?"s":"")+" on this plan. Send it back, or delete them first.");return}
    if(V.dataset.verdict==="changes"&&!n){alert("Request changes needs at least one comment — say what is wrong.");return}
    submitReview(V.dataset.verdict);return}
  const C=e.target.closest("[data-repo]");if(C){fRepo=C.dataset.repo;save();draw();return}
  const S=e.target.closest("[data-state]");if(S){fState=S.dataset.state;save();draw();return}
  const row=e.target.closest("[data-open]");
  if(row){const b=document.getElementById("b-"+row.dataset.open);if(b)b.classList.toggle("open")}});
document.addEventListener("keydown",e=>{
  if(e.key==="Escape"&&openPlan){closeReader();return}
  if((e.key==="Enter"||e.key===" ")&&e.target.classList&&e.target.classList.contains("num")){
    e.preventDefault();e.target.click()}});
document.getElementById("q").addEventListener("input",e=>{q=e.target.value.toLowerCase();renderRepos()});
load();
