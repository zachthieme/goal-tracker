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
    { placeholder: { options: { name: "title", type: "title", x: 0.7, y: 2.1, w: 6.2, h: 1.5, fontSize: 54, bold: true, color: C.background1, align: "left", valign: "top", margin: 0 }, text: "" } },
    { placeholder: { options: { name: "body", type: "body", x: 0.7, y: 3.75, w: 5.6, h: 1.6, fontSize: 22, color: C.accent6, align: "left", valign: "top", margin: 0 }, text: "" } },
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
const demoNote = (slide, x) => slide.addText("Screenshot from the product, on a seeded 50-goal demo org", { x, y: 6.62, w: 7.6, h: 0.28, fontSize: 10, italic: true, color: MUTED, margin: 0, isTextBox: true, objectName: "Caption" });

// 1 Title
pres.addSection({ title: "Pitch" });
let s = pres.addSlide({ masterName: "Dark", sectionTitle: "Pitch" });
s.addText("Goal Tracker", { placeholder: "title" });
s.addText("Find the goals that are in trouble before the review does.", { placeholder: "body" });
mark(s, "green", 0.7, 1.45, 0.3, "Green mark"); mark(s, "yellow", 1.15, 1.45, 0.3, "Yellow mark"); mark(s, "red", 1.6, 1.45, 0.3, "Red mark");
shot(s, "risks", 6.9, 1.9, 5.85, "Risks page", "The Risks page listing ten goals that need attention");
s.addNotes("Goal Tracker is one place for the org to state its goals, update them weekly, and produce review-ready reports. The pitch in one line: leaders find trouble without waiting for the review or for someone to tell them.");

// 2 Problem
s = pres.addSlide({ masterName: "Content", sectionTitle: "Pitch" });
s.addText("Trouble surfaces at the review, or after it", { placeholder: "title" });
const probs = [
  { mark: "stale", head: "Silence looks like Green", body: "A goal nobody has updated in six weeks still shows its last good status. Nothing flags it." },
  { mark: "yellow", head: "Reviews run on retyped status", body: "Each review is assembled by hand from docs, slides and chat. The work goes into collecting, not reading." },
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
s.addNotes("Three failure modes we see with status tracking today. Adjust the wording to your org's own examples before presenting.");

// 3 How it works
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

// 4 Goals
pres.addSection({ title: "Product tour" });
s = pres.addSlide({ masterName: "Content", sectionTitle: "Product tour" });
s.addText("Every goal in one list, problems first", { placeholder: "title" });
shot(s, "goals", 0.6, 1.6, 7.9, "Goals page", "The Goals list sorted with Red goals at the top");
demoNote(s, 0.6);
points(s, [
  { mark: "red", head: "Sorted by Health", body: "Red first, then Stale, then Yellow. The shape of the org reads in one screen." },
  { mark: "yellow", head: "Slips stay visible", body: "A moved date shows the old date struck through beside the new one." },
  { mark: "green", head: "Slice it your way", body: "Filter and group by team, pillar, quarter, or any Dimension an Admin defines." },
], 9.0, 1.75, 3.73);
s.addNotes("This is the data view: what exists and how each goal is doing. One goal type at every level, from org outcome to team project, linked by what contributes to what.");

// 5 Risks
s = pres.addSlide({ masterName: "Content", sectionTitle: "Product tour" });
s.addText("A silent goal is as loud as a Red one", { placeholder: "title" });
shot(s, "risks", 4.83, 1.6, 7.9, "Risks page", "The Risks page with ten flagged goals in three groups");
demoNote(s, 4.83);
points(s, [
  { mark: "stale", head: "Stale is a first-class signal", body: "A goal past its check-in cadence is flagged, even when its last status was Green." },
  { mark: "yellow", head: "Grouped by who must act", body: "Owner needs to update, plan doesn't fit, or needs an Admin." },
  { mark: "green", head: "One fix per row", body: "Nudge the owner, suggest a parent, or compare dates. No looking up who owns what." },
], 0.6, 1.75, 3.85);
s.addNotes("This is the context view: which goals need attention, why, and who has to act. The count sits in the top bar on every page. In the demo org, 10 of 50 goals are flagged, and 4 of those are Green goals nobody has updated in three to six weeks.");

// 6 Goal page
s = pres.addSlide({ masterName: "Content", sectionTitle: "Product tour" });
s.addText("Bad news comes with a plan and a date", { placeholder: "title" });
shot(s, "goal", 0.6, 1.6, 7.9, "Goal page", "A Red goal page showing latest status, Path to Green and milestones");
demoNote(s, 0.6);
points(s, [
  { mark: "red", head: "Path to Green is required", body: "A Yellow or Red check-in must state the plan, or the help needed, and a back-to-Green date." },
  { mark: "yellow", head: "Overdue plans are flagged", body: "If that date passes and the goal still isn't Green, it is flagged as loudly as Red." },
  { mark: "green", head: "The history is kept", body: "Every check-in, date slip and ownership change, week by week." },
], 9.0, 1.75, 3.73);
s.addNotes("Open any flagged goal to see the owner's latest status and plan, the worst Health among its child goals beside the owner's own call, and how it got here.");

// 7 Check-in video
s = pres.addSlide({ masterName: "Content", sectionTitle: "Product tour" });
s.addText("A check-in takes a manager about a minute", { placeholder: "title" });
s.addShape(pres.shapes.RECTANGLE, { x: 0.6, y: 1.6, w: 8.0, h: 4.5, fill: { color: C.background1 }, line: { color: C.accent6, width: 1 }, shadow: { type: "outer", color: "134E4A", opacity: 0.18, blur: 12, offset: 4, angle: 90 }, objectName: "Video frame" });
s.addMedia({ type: "video", path: join(ASSETS, "checkin.mp4"), cover: "data:image/png;base64," + readFileSync(join(ASSETS, "shots", "checkin-filled.png")).toString("base64"), x: 0.6, y: 1.6, w: 8.0, h: 4.5, objectName: "Check-in video" });
s.addText("20-second recording of a real check-in on the demo org. Click to play.", { x: 0.6, y: 6.25, w: 8, h: 0.28, fontSize: 10, italic: true, color: MUTED, margin: 0, isTextBox: true, objectName: "Caption" });
points(s, [
  { mark: "green", head: "Home says what's due", body: "\"Your week\" lists the check-ins that need you, and why each is listed." },
  { mark: "green", head: "No change is one click", body: "A routine week records the same Health and status with today's date." },
  { mark: "yellow", head: "Hard to get wrong", body: "The form starts from last week's answers and asks for a plan only when Health isn't Green." },
], 9.1, 1.75, 3.63);
s.addNotes("The video: an owner lands on Home, sees one Stale goal, opens the check-in, marks it Yellow with a Path to Green, and submits. This is the price of everything on the previous slides, so it has to stay low.");

// 8 Report
s = pres.addSlide({ masterName: "Content", sectionTitle: "Product tour" });
s.addText("The monthly review writes itself", { placeholder: "title" });
shot(s, "report-published", 4.83, 1.6, 7.9, "Published report", "A published Growth monthly review with a Health summary and a Red goal in full");
demoNote(s, 4.83);
points(s, [
  { mark: "red", head: "Exceptions in full", body: "So What, status, Path to Green and milestones for each. Unchanged Green goals take one line." },
  { mark: "yellow", head: "Shows what changed", body: "Each publication is a frozen snapshot. The next one shows what moved since." },
  { mark: "green", head: "Questions reach the owner", body: "Comments route to the goal's owner. Action Items carry forward until closed." },
], 0.6, 1.75, 3.85);
s.addNotes("Anyone can build a Report over any goals, by rule (Team is Growth) or by hand-picking. The author writes only the introduction and their own notes. It shares from the tool, as Markdown, or printed to PDF.");

// 9 Ask
pres.addSection({ title: "Ask" });
s = pres.addSlide({ masterName: "Closing", sectionTitle: "Ask" });
s.addText("The ask: pilot one org for one review cycle", { placeholder: "title" });
const plan = [
  { n: "1", head: "Load one org's goals", body: "Owners state each goal, its So What, and what it contributes to." },
  { n: "2", head: "Check in weekly for a month", body: "About a minute per goal. Reminders and nudges do the chasing." },
  { n: "3", head: "Run the review from the Report", body: "No hand-built status deck. It publishes from the check-ins." },
];
plan.forEach((p, i) => {
  const x = 0.7 + i * 4.1;
  s.addShape(pres.shapes.ROUNDED_RECTANGLE, { x, y: 2.0, w: 3.7, h: 2.9, rectRadius: 0.08, fill: { color: C.accent1 }, line: { color: C.accent1, width: 0 }, objectName: `Plan ${i + 1} card` });
  s.addText(p.n, { x: x + 0.3, y: 2.2, w: 1, h: 0.8, fontSize: 44, bold: true, color: C.accent6, margin: 0, isTextBox: true, objectName: `Plan ${i + 1} number` });
  s.addText(p.head, { x: x + 0.3, y: 3.05, w: 3.1, h: 0.75, fontSize: 20, bold: true, color: C.background1, margin: 0, valign: "top", isTextBox: true, objectName: `Plan ${i + 1} heading` });
  s.addText(p.body, { x: x + 0.3, y: 3.82, w: 3.1, h: 0.9, fontSize: 14, color: C.background1, margin: 0, valign: "top", isTextBox: true, objectName: `Plan ${i + 1} text` });
});
s.addText([
  { text: "Judge it by one question: ", options: { bold: true, color: C.background1 } },
  { text: "did we learn about trouble before the review, with a plan attached?", options: { color: C.accent6 } },
], { x: 0.7, y: 5.5, w: 11.9, h: 0.9, fontSize: 22, margin: 0, valign: "middle", isTextBox: true, objectName: "Success test" });
s.addNotes("The ask is small and time-boxed: one org, one review cycle. Fill in which org, the start date, and who the sponsor is before presenting.");

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
