#!/usr/bin/env node
// build.mjs: writes the pitch deck from the assets capture.mjs took.
//
//   node docs/pitch/build.mjs [assets-dir] [out.pptx]
//
// The defaults are docs/pitch/assets and docs/pitch/goal-tracker-pitch.pptx.
// It needs this directory's packages: run `npm ci` here first.
//
// pptxgenjs can't write a theme's colours, so the deck is written with scheme
// colours and applyTheme then puts THEME's values into the theme part. Change
// a colour in THEME and every slide follows.
import { readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const pptxgen = require("pptxgenjs");
const JSZip = require("jszip");

const here = dirname(fileURLToPath(import.meta.url));
const ASSETS = resolve(process.argv[2] ?? join(here, "assets"));
const OUT = resolve(process.argv[3] ?? join(here, "goal-tracker-pitch.pptx"));

const THEME = {
  name: "Goal Tracker",
  headFontFace: "Calibri",
  bodyFontFace: "Calibri",
  colors: {
    dk1: "1A1F1E", lt1: "FFFFFF", dk2: "134E4A", lt2: "EEF4F3",
    accent1: "0F766E", accent2: "A32020", accent3: "C98A04", accent4: "1F6B45",
    accent5: "5B6B69", accent6: "CFE3E0", hlink: "0F766E", folHlink: "5B6B69",
  },
};
const pres = new pptxgen();
pres.layout = "LAYOUT_WIDE";
pres.title = "Goal Tracker";
pres.author = "Zach Thieme";
pres.theme = { headFontFace: THEME.headFontFace, bodyFontFace: THEME.bodyFontFace };
const C = pres.SchemeColor;
const TEAL = C.accent1, RED = C.accent2, YEL = C.accent3, GRN = C.accent4, MUTED = C.accent5;

pres.defineSlideMaster({
  title: "Dark",
  background: { color: C.text2 },
  objects: [
    { placeholder: { options: { name: "title", type: "title", x: 0.7, y: 2.1, w: 5.3, h: 1.5, fontSize: 54, bold: true, color: C.background1, align: "left", valign: "top", margin: 0 }, text: "" } },
    { placeholder: { options: { name: "body", type: "body", x: 0.7, y: 3.75, w: 5.1, h: 1.6, fontSize: 22, color: C.accent6, align: "left", valign: "top", margin: 0 }, text: "" } },
  ],
});
pres.defineSlideMaster({
  title: "Content",
  background: { color: C.background1 },
  objects: [
    { placeholder: { options: { name: "title", type: "title", x: 0.6, y: 0.45, w: 12.1, h: 0.9, fontSize: 36, bold: true, color: C.text2, align: "left", valign: "middle", margin: 0 }, text: "" } },
    { text: { text: "Goal Tracker", options: { x: 0.6, y: 6.98, w: 3, h: 0.3, fontSize: 10, color: MUTED, margin: 0 } } },
  ],
  slideNumber: { x: 12.2, y: 6.98, w: 0.55, h: 0.3, fontSize: 10, color: MUTED, align: "right" },
});
pres.defineSlideMaster({
  title: "Closing",
  background: { color: C.text2 },
  objects: [
    { placeholder: { options: { name: "title", type: "title", x: 0.7, y: 0.6, w: 12, h: 1.0, fontSize: 40, bold: true, color: C.background1, align: "left", valign: "middle", margin: 0 }, text: "" } },
  ],
});

// The app's own Health marks are the deck's motif: circle, triangle, square.
function mark(slide, kind, x, y, s, name) {
  const shape = { green: pres.shapes.OVAL, yellow: pres.shapes.ISOSCELES_TRIANGLE, red: pres.shapes.RECTANGLE, stale: pres.shapes.RECTANGLE }[kind];
  const color = { green: GRN, yellow: YEL, red: RED, stale: MUTED }[kind];
  slide.addShape(shape, { x, y, w: s, h: s, fill: { color }, line: { color, width: 0 }, objectName: name });
}
function shot(slide, file, x, y, w, name, alt) {
  const h = w * 0.625;
  slide.addShape(pres.shapes.RECTANGLE, { x, y, w, h, fill: { color: C.background1 }, line: { color: C.accent6, width: 1 }, shadow: { type: "outer", color: "134E4A", opacity: 0.18, blur: 12, offset: 4, angle: 90 }, objectName: name + " frame" });
  slide.addImage({ path: join(ASSETS, "shots", `${file}.png`), x, y, w, h, objectName: name, altText: alt });
}
function points(slide, items, x, y, w, gap = 1.55) {
  items.forEach((it, i) => {
    const yy = y + i * gap;
    mark(slide, it.mark, x, yy + 0.07, 0.2, `Point ${i + 1} mark`);
    slide.addText(it.head, { x: x + 0.38, y: yy, w: w - 0.38, h: 0.36, fontSize: 18, bold: true, color: C.text1, margin: 0, valign: "top", isTextBox: true, objectName: `Point ${i + 1} heading` });
    slide.addText(it.body, { x: x + 0.38, y: yy + 0.4, w: w - 0.38, h: gap - 0.55, fontSize: 14, color: MUTED, margin: 0, valign: "top", isTextBox: true, objectName: `Point ${i + 1} text` });
  });
}
const demoNote = (slide, x) => slide.addText("Screenshots from the product, on a seeded 50-goal demo org", { x, y: 6.62, w: 7.6, h: 0.28, fontSize: 10, italic: true, color: MUTED, margin: 0, isTextBox: true, objectName: "Caption" });

// 1 Title
pres.addSection({ title: "Pitch" });
let s = pres.addSlide({ masterName: "Dark", sectionTitle: "Pitch" });
s.addText("Goal Tracker", { placeholder: "title" });
s.addText("Find the goals that are in trouble before the review does.", { placeholder: "body" });
mark(s, "green", 0.7, 1.45, 0.3, "Green mark"); mark(s, "yellow", 1.15, 1.45, 0.3, "Yellow mark"); mark(s, "red", 1.6, 1.45, 0.3, "Red mark");
shot(s, "risks-top", 6.3, 1.6, 6.43, "Risks page", "The Risks page: ten goals need attention, four of them Stale");
s.addNotes("Goal Tracker is one place for the org to state its goals, update them weekly, and produce review-ready reports. The pitch in one line: leaders find trouble without waiting for the review or for someone to tell them.");

// 2 The ask, up front
s = pres.addSlide({ masterName: "Content", sectionTitle: "Pitch" });
s.addText("The ask: replace one status deck for one cycle", { placeholder: "title" });
const asks = [
  { big: "One org", body: "Its goals move out of the PowerPoint status deck and into Goal Tracker." },
  { big: "One cycle", body: "Owners check in weekly. The review runs from a published Report." },
  { big: "One decision", body: "At the end we expand it, extend the pilot, or stop." },
];
asks.forEach((p, i) => {
  const x = 0.6 + i * 4.18;
  s.addShape(pres.shapes.ROUNDED_RECTANGLE, { x, y: 1.9, w: 3.78, h: 2.9, rectRadius: 0.08, fill: { color: C.background2 }, line: { color: C.background2, width: 0 }, objectName: `Ask ${i + 1} card` });
  s.addText(p.big, { x: x + 0.35, y: 2.25, w: 3.1, h: 0.8, fontSize: 30, bold: true, color: TEAL, margin: 0, valign: "top", isTextBox: true, objectName: `Ask ${i + 1} heading` });
  s.addText(p.body, { x: x + 0.35, y: 3.15, w: 3.1, h: 1.4, fontSize: 16, color: C.text1, margin: 0, valign: "top", isTextBox: true, objectName: `Ask ${i + 1} text` });
});
s.addText("The rest of this deck: why the status deck falls short, what the product does, what is ready, and how we would judge the pilot.", { x: 0.6, y: 5.3, w: 12.1, h: 0.8, fontSize: 16, italic: true, color: C.text2, margin: 0, valign: "top", isTextBox: true, objectName: "Roadmap" });
s.addNotes("Say the ask first so the rest of the deck is heard as evidence for it. Name the org and the start date here once they are chosen.");

// 3 Problem
s = pres.addSlide({ masterName: "Content", sectionTitle: "Pitch" });
s.addText("Trouble surfaces at the review, or after it", { placeholder: "title" });
const probs = [
  { mark: "stale", head: "Silence looks like Green", body: "A goal nobody has updated keeps the colour it had last time. Nothing flags it." },
  { mark: "yellow", head: "The deck is rebuilt by hand", body: "Each review, status is collected and retyped into slides. The work goes into assembling, not reading." },
  { mark: "red", head: "Bad news arrives without a plan", body: "A Red status says something is wrong. It rarely says what the owner will do, or by when." },
];
probs.forEach((p, i) => {
  const x = 0.6 + i * 4.18;
  s.addShape(pres.shapes.ROUNDED_RECTANGLE, { x, y: 1.9, w: 3.78, h: 3.9, rectRadius: 0.08, fill: { color: C.background2 }, line: { color: C.background2, width: 0 }, objectName: `Problem ${i + 1} card` });
  mark(s, p.mark, x + 0.35, 2.3, 0.42, `Problem ${i + 1} mark`);
  s.addText(p.head, { x: x + 0.35, y: 3.05, w: 3.1, h: 0.8, fontSize: 22, bold: true, color: C.text1, margin: 0, valign: "top", isTextBox: true, objectName: `Problem ${i + 1} heading` });
  s.addText(p.body, { x: x + 0.35, y: 3.95, w: 3.1, h: 1.6, fontSize: 15, color: MUTED, margin: 0, valign: "top", isTextBox: true, objectName: `Problem ${i + 1} text` });
});
s.addText("Goal Tracker is built around one question: does this make trouble easier to find, or harder to hide?", { x: 0.6, y: 6.05, w: 12.1, h: 0.5, fontSize: 16, italic: true, color: C.text2, margin: 0, isTextBox: true, objectName: "Takeaway" });
s.addNotes("Three ways tracking goals in slides lets trouble hide. Replace these with a real incident from our own reviews before presenting.");

// 4 Why not the deck we have
s = pres.addSlide({ masterName: "Content", sectionTitle: "Pitch" });
s.addText("What PowerPoint can't do for us", { placeholder: "title" });
const cmpX = [0.6, 3.35, 8.05], cmpW = [2.55, 4.5, 4.68];
s.addText("PowerPoint status deck", { x: cmpX[1], y: 1.6, w: cmpW[1], h: 0.5, fontSize: 16, bold: true, color: MUTED, margin: 0, valign: "middle", isTextBox: true, objectName: "Today heading" });
s.addText("Goal Tracker", { x: cmpX[2], y: 1.6, w: cmpW[2], h: 0.5, fontSize: 16, bold: true, color: TEAL, margin: 0, valign: "middle", isTextBox: true, objectName: "Goal Tracker heading" });
const rows = [
  ["A goal nobody updated", "Keeps its last colour. Nobody is told.", "Flagged Stale, as prominently as Red."],
  ["Bad news", "A red box. A plan is optional.", "Needs a Path to Green and a back-to-Green date."],
  ["The review document", "Assembled by hand every cycle.", "Generated from check-ins, marking what changed."],
  ["History and follow-ups", "Spread across old decks and notes.", "Kept on the goal. Action Items carry forward."],
];
rows.forEach((r, i) => {
  const y = 2.2 + i * 1.08;
  s.addText(r[0], { x: cmpX[0], y, w: cmpW[0], h: 0.93, fontSize: 16, bold: true, color: C.text1, margin: 0, valign: "middle", isTextBox: true, objectName: `Row ${i + 1} label` });
  s.addShape(pres.shapes.ROUNDED_RECTANGLE, { x: cmpX[1], y, w: cmpW[1], h: 0.93, rectRadius: 0.06, fill: { color: C.background2 }, line: { color: C.background2, width: 0 }, objectName: `Row ${i + 1} today card` });
  s.addText(r[1], { x: cmpX[1] + 0.25, y, w: cmpW[1] - 0.5, h: 0.93, fontSize: 15, color: MUTED, margin: 0, valign: "middle", isTextBox: true, objectName: `Row ${i + 1} today` });
  s.addShape(pres.shapes.ROUNDED_RECTANGLE, { x: cmpX[2], y, w: cmpW[2], h: 0.93, rectRadius: 0.06, fill: { color: TEAL }, line: { color: TEAL, width: 0 }, objectName: `Row ${i + 1} Goal Tracker card` });
  s.addText(r[2], { x: cmpX[2] + 0.25, y, w: cmpW[2] - 0.5, h: 0.93, fontSize: 15, bold: true, color: C.background1, margin: 0, valign: "middle", isTextBox: true, objectName: `Row ${i + 1} Goal Tracker` });
});
s.addNotes("The first objection is 'why not keep the deck we have'. Slides are a fine way to present status and a poor way to track it: a slide can't notice that it is out of date. Check the left column against how our decks are really kept, and reword any row that isn't true of us.");

// 5 How it works
s = pres.addSlide({ masterName: "Content", sectionTitle: "Pitch" });
s.addText("One mechanism, from weekly update to review", { placeholder: "title" });
const steps = [
  { n: "1", head: "Owners check in weekly", body: "Health, a short status, and any date changes. A routine week is one click: No change." },
  { n: "2", head: "Risks flags what needs attention", body: "Red, Stale, unaligned and conflicting goals, grouped by who has to act, each with one fix." },
  { n: "3", head: "The Report writes itself", body: "Exceptions in full, on-track goals in one line each, with what changed since the last review." },
];
steps.forEach((p, i) => {
  const x = 0.6 + i * 4.18;
  s.addShape(pres.shapes.OVAL, { x, y: 1.95, w: 0.8, h: 0.8, fill: { color: TEAL }, line: { color: TEAL, width: 0 }, objectName: `Step ${i + 1} circle` });
  s.addText(p.n, { x, y: 1.95, w: 0.8, h: 0.8, fontSize: 28, bold: true, color: C.background1, align: "center", valign: "middle", margin: 0, isTextBox: true, objectName: `Step ${i + 1} number` });
  if (i < 2) s.addShape(pres.shapes.LINE, { x: x + 1.0, y: 2.35, w: 2.98, h: 0, line: { color: C.accent6, width: 2, endArrowType: "triangle" }, objectName: `Arrow ${i + 1}` });
  s.addText(p.head, { x, y: 3.0, w: 3.6, h: 0.8, fontSize: 22, bold: true, color: C.text1, margin: 0, valign: "top", isTextBox: true, objectName: `Step ${i + 1} heading` });
  s.addText(p.body, { x, y: 3.85, w: 3.6, h: 1.3, fontSize: 15, color: MUTED, margin: 0, valign: "top", isTextBox: true, objectName: `Step ${i + 1} text` });
});
s.addShape(pres.shapes.ROUNDED_RECTANGLE, { x: 0.6, y: 5.45, w: 12.13, h: 1.1, rectRadius: 0.08, fill: { color: C.background2 }, line: { color: C.background2, width: 0 }, objectName: "Positioning band" });
s.addText([
  { text: "The rigor of an MBR, the ease of a 5-15, the org-wide view of OKRs.", options: { bold: true, color: C.text2 } },
  { text: "  Managers supply the data cheaply; leaders read it easily.", options: { color: MUTED } },
], { x: 0.9, y: 5.45, w: 11.5, h: 1.1, fontSize: 17, valign: "middle", margin: 0, isTextBox: true, objectName: "Positioning text" });
s.addNotes("Two audiences. Frontline and mid-level managers own goals and check in. VPs and above read. The manager side has to be cheap or the data goes stale; the leader side has to be easy to read.");

// 6 Find trouble
pres.addSection({ title: "Product" });
s = pres.addSlide({ masterName: "Content", sectionTitle: "Product" });
s.addText("Find trouble: a silent goal is as loud as a Red one", { placeholder: "title" });
shot(s, "risks", 4.83, 1.6, 7.9, "Risks page", "The Risks page with ten flagged goals in three groups");
demoNote(s, 4.83);
points(s, [
  { mark: "stale", head: "Stale is a first-class signal", body: "A goal past its check-in cadence is flagged, even when its last status was Green." },
  { mark: "yellow", head: "Grouped by who must act", body: "Owner needs to update, plan doesn't fit, or needs an Admin." },
  { mark: "green", head: "One fix per row", body: "Nudge the owner, suggest a parent, or compare dates. No looking up who owns what." },
], 0.6, 1.75, 3.85);
s.addNotes("This is what a leader opens between reviews: which goals need attention, why, and who has to act. The count sits in the top bar on every page. In the demo org, 10 of 50 goals are flagged, and 4 of those are Green goals nobody has updated in three to six weeks.");

// 7 Understand it
s = pres.addSlide({ masterName: "Content", sectionTitle: "Product" });
s.addText("Understand it: the plan, the date, the review", { placeholder: "title" });
shot(s, "goal", 0.6, 1.6, 5.9, "Goal page", "A Red goal's page showing its latest status and Path to Green");
shot(s, "report-published", 6.83, 1.6, 5.9, "Published report", "A published monthly review with a Health summary and a Red goal in full");
[
  { x: 0.6, mark: "red", head: "Bad news comes with a plan", body: "A Yellow or Red check-in must state a Path to Green and a back-to-Green date. Miss that date and the goal is flagged." },
  { x: 6.83, mark: "green", head: "The review writes itself", body: "Exceptions in full, on-track goals in one line, and what changed since the last one. Comments reach the goal's owner." },
].forEach((p, i) => {
  mark(s, p.mark, p.x, 5.62, 0.2, `Caption ${i + 1} mark`);
  s.addText(p.head, { x: p.x + 0.38, y: 5.55, w: 5.5, h: 0.36, fontSize: 18, bold: true, color: C.text1, margin: 0, valign: "top", isTextBox: true, objectName: `Caption ${i + 1} heading` });
  s.addText(p.body, { x: p.x + 0.38, y: 5.93, w: 5.5, h: 0.62, fontSize: 14, color: MUTED, margin: 0, valign: "top", isTextBox: true, objectName: `Caption ${i + 1} text` });
});
demoNote(s, 0.6);
s.addNotes("Left: any flagged goal opens to the owner's latest status and plan, the worst Health among its child goals beside the owner's own call, and the history of how it got here. Right: a Report built by one rule, Team is Growth, and published as it stands. The author writes only the introduction and their own notes. It shares from the tool, as Markdown, or printed to PDF.");

// 8 What it costs managers
s = pres.addSlide({ masterName: "Content", sectionTitle: "Product" });
s.addText("The cost: most weeks, one click per goal", { placeholder: "title" });
s.addShape(pres.shapes.RECTANGLE, { x: 0.6, y: 1.6, w: 8.0, h: 4.5, fill: { color: C.background1 }, line: { color: C.accent6, width: 1 }, shadow: { type: "outer", color: "134E4A", opacity: 0.18, blur: 12, offset: 4, angle: 90 }, objectName: "Video frame" });
s.addMedia({ type: "video", path: join(ASSETS, "checkin.mp4"), cover: "data:image/png;base64," + readFileSync(join(ASSETS, "shots", "checkin-filled.png")).toString("base64"), x: 0.6, y: 1.6, w: 8.0, h: 4.5, objectName: "Check-in video" });
s.addText("Recording from the demo org: a one-click No change, then a full check-in. Click to play.", { x: 0.6, y: 6.25, w: 8, h: 0.28, fontSize: 10, italic: true, color: MUTED, margin: 0, isTextBox: true, objectName: "Caption" });
points(s, [
  { mark: "green", head: "Home says what's due", body: "\"Your week\" lists the check-ins that need you, and why each is listed." },
  { mark: "green", head: "No change is one click", body: "A routine week records the same Health and status with today's date." },
  { mark: "yellow", head: "A real update is one form", body: "It starts from last week's answers and asks for a plan only when Health isn't Green." },
], 9.1, 1.75, 3.63);
s.addNotes("The video: an owner lands on Home, clears a Stale goal with one click on No change, then checks in Yellow on another with a Path to Green. No change is refused when it would be wrong, such as a Green goal with an overdue milestone. This is the price of everything on the previous slides, so it has to stay low. We have not measured it with real managers yet; the pilot would.");

// 9 Readiness
pres.addSection({ title: "Decision" });
s = pres.addSlide({ masterName: "Content", sectionTitle: "Decision" });
s.addText("What's built, and what a pilot still needs", { placeholder: "title" });
const cols = [
  { x: 0.6, mark: "green", head: "Working today", items: [
    "Goals, the links between them, and weekly check-ins",
    "Risks: Stale, unaligned, schedule conflicts, ownerless",
    "Reports by rule or hand-picked, with Markdown and PDF",
    "Comments and Action Items on published reports",
    "Spreadsheet import to load an org's goals",
  ] },
  { x: 6.87, mark: "yellow", head: "Needed before a pilot", items: [
    "Company sign-in. Today any email address signs in.",
    "Sending email. Reminders and alerts are logged, not sent.",
    "Hosting, and someone to run it.",
  ] },
];
cols.forEach((c, ci) => {
  s.addShape(pres.shapes.ROUNDED_RECTANGLE, { x: c.x, y: 1.7, w: 5.87, h: 4.75, rectRadius: 0.06, fill: { color: C.background2 }, line: { color: C.background2, width: 0 }, objectName: `Readiness ${ci + 1} card` });
  s.addText(c.head, { x: c.x + 0.35, y: 1.95, w: 5.2, h: 0.5, fontSize: 22, bold: true, color: C.text2, margin: 0, valign: "middle", isTextBox: true, objectName: `Readiness ${ci + 1} heading` });
  c.items.forEach((t, i) => {
    const y = 2.7 + i * 0.72;
    mark(s, c.mark, c.x + 0.35, y + 0.05, 0.18, `Readiness ${ci + 1} mark ${i + 1}`);
    s.addText(t, { x: c.x + 0.72, y, w: 4.85, h: 0.6, fontSize: 15, color: C.text1, margin: 0, valign: "top", isTextBox: true, objectName: `Readiness ${ci + 1} item ${i + 1}` });
  });
});
s.addNotes("It is a working prototype. Everything shown in this deck was captured from the running product. The three gaps on the right are what stand between it and real users: sign-in is a development one, emails are written to a log, and it has no production home.");

// 10 Objections
s = pres.addSlide({ masterName: "Content", sectionTitle: "Decision" });
s.addText("Three ways this could fail, and the answer", { placeholder: "title" });
const risks = [
  { q: "Managers won't check in", a: "A routine week is one click. A weekly reminder lists what's due, and anyone can nudge the owner of a Stale goal." },
  { q: "It's one more tool", a: "It replaces the status deck. The Report is the review document, and it exports to Markdown or PDF." },
  { q: "Owners will call it Green", a: "The worst Health among a goal's children shows beside the owner's call, and a difference must be explained. Stale goals and slipped dates show whatever the owner says." },
];
risks.forEach((r, i) => {
  const y = 1.75 + i * 1.6;
  s.addShape(pres.shapes.ROUNDED_RECTANGLE, { x: 0.6, y, w: 4.0, h: 1.35, rectRadius: 0.06, fill: { color: C.background2 }, line: { color: C.background2, width: 0 }, objectName: `Objection ${i + 1} card` });
  s.addText(`"${r.q}"`, { x: 0.9, y, w: 3.4, h: 1.35, fontSize: 20, bold: true, color: C.text2, margin: 0, valign: "middle", isTextBox: true, objectName: `Objection ${i + 1}` });
  s.addShape(pres.shapes.LINE, { x: 4.75, y: y + 0.675, w: 0.6, h: 0, line: { color: C.accent6, width: 2, endArrowType: "triangle" }, objectName: `Objection ${i + 1} arrow` });
  s.addText(r.a, { x: 5.55, y, w: 7.15, h: 1.35, fontSize: 16, color: C.text1, margin: 0, valign: "middle", isTextBox: true, objectName: `Answer ${i + 1}` });
});
s.addNotes("The first is the real risk: the tool is only as good as the check-ins. The reminder depends on email being sent, which is one of the gaps on the previous slide. The pilot's first measure tests exactly this.");

// 11 The pilot
s = pres.addSlide({ masterName: "Closing", sectionTitle: "Decision" });
s.addText("The pilot: one org, one cycle, three measures", { placeholder: "title" });
const sub = (text, x, name) => s.addText(text, { x, y: 1.85, w: 5.6, h: 0.4, fontSize: 14, bold: true, color: C.accent6, charSpacing: 2, margin: 0, isTextBox: true, objectName: name });
sub("THE PLAN", 0.7, "Plan label");
sub("HOW WE'D JUDGE IT", 6.95, "Measures label");
const plan = [
  { head: "Load one org's goals", body: "Each with its So What and what it contributes to." },
  { head: "Check in weekly for a month", body: "Reminders and nudges do the chasing." },
  { head: "Run the review from the Report", body: "No hand-built status deck that cycle." },
];
const measures = [
  { head: "Check-ins on time", body: "Goals updated each week. Proposed bar: 80%." },
  { head: "Trouble found early", body: "Problems surfaced before the review, not at it." },
  { head: "Review prep time", body: "Hours to produce the Report, against the last deck." },
];
[plan, measures].forEach((list, ci) => {
  const x = ci === 0 ? 0.7 : 6.95;
  list.forEach((p, i) => {
    const y = 2.45 + i * 1.12;
    s.addShape(pres.shapes.ROUNDED_RECTANGLE, { x, y, w: 5.68, h: 0.97, rectRadius: 0.06, fill: { color: C.accent1 }, line: { color: C.accent1, width: 0 }, objectName: `${ci === 0 ? "Plan" : "Measure"} ${i + 1} card` });
    s.addText(String(i + 1), { x: x + 0.25, y, w: 0.5, h: 0.97, fontSize: 28, bold: true, color: C.accent6, margin: 0, valign: "middle", isTextBox: true, objectName: `${ci === 0 ? "Plan" : "Measure"} ${i + 1} number` });
    s.addText([
      { text: p.head, options: { bold: true, fontSize: 17, color: C.background1, breakLine: true } },
      { text: p.body, options: { fontSize: 14, color: C.background1 } },
    ], { x: x + 0.8, y, w: 4.7, h: 0.97, margin: 0, valign: "middle", isTextBox: true, objectName: `${ci === 0 ? "Plan" : "Measure"} ${i + 1} text` });
  });
});
s.addText([
  { text: "Then one decision: ", options: { bold: true, color: C.background1 } },
  { text: "expand it, extend the pilot, or stop.", options: { color: C.accent6 } },
], { x: 0.7, y: 6.0, w: 11.9, h: 0.7, fontSize: 22, margin: 0, valign: "middle", isTextBox: true, objectName: "Decision" });
s.addNotes("The ask is small and time-boxed. Fill in the org, the start date and the sponsor before presenting. The 80% bar is a proposal; agree the bar with the VP. For the third measure, note how long the last PowerPoint status deck took to assemble before the pilot starts.");

// applyTheme replaces the colour scheme pptxgenjs wrote (Office's) with
// THEME's, and names the theme after it.
async function applyTheme(deck, theme) {
  const zip = await JSZip.loadAsync(readFileSync(deck));
  const part = "ppt/theme/theme1.xml";
  const slots = Object.entries(theme.colors)
    .map(([slot, hex]) => `<a:${slot}><a:srgbClr val="${hex}"/></a:${slot}>`)
    .join("");
  const xml = (await zip.file(part).async("string"))
    .replace(/<a:clrScheme\b[\s\S]*?<\/a:clrScheme>/, () => `<a:clrScheme name="${theme.name}">${slots}</a:clrScheme>`)
    .replace(/(<a:(?:theme|fontScheme)\b[^>]*?\bname=")[^"]*"/g, (_, start) => `${start}${theme.name}"`);
  zip.file(part, xml);
  writeFileSync(deck, await zip.generateAsync({ type: "nodebuffer", compression: "DEFLATE" }));
}

await pres.writeFile({ fileName: OUT });
await applyTheme(OUT, THEME);
console.log(`wrote ${OUT}`);
