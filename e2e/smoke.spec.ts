// The harness's smoke test: the seeded app comes up, and the development
// sign-in lands an Admin and a lead on Home.
import { admin, expect, signIn, test } from "./fixtures";

test("an Admin signs in to Home with Follow-ups in the top bar", async ({ page }) => {
  await signIn(page, admin);
  await expect(page).toHaveURL(/\/home$/);
  await expect(page.getByRole("heading", { level: 1, name: "Your week" })).toBeVisible();
  await expect(page.getByTestId("current-user")).toContainText("(Admin)");
  await expect(page.getByTestId("nav-follow-ups")).toHaveText(/^Follow-ups/);
});

test("a lead signs in and sees Your week", async ({ page }) => {
  await signIn(page, "platform-lead@example.com");
  await expect(page.getByRole("heading", { level: 1, name: "Your week" })).toBeVisible();
  await expect(page.getByTestId("home-your-goals")).toBeVisible();
});

// Home sits in the flat frame: a page header reading Home with New goal at
// its right, and its sections on the one surface, with no card.
test("Home has a page header with New goal, and no card", async ({ page }) => {
  await signIn(page, "platform-lead@example.com");
  const header = page.locator(".page-header");
  await expect(header.getByRole("heading", { level: 1, name: "Home" })).toBeVisible();
  await expect(header.getByRole("link", { name: "New goal" })).toHaveAttribute("href", "/goals/new");
  await expect(page.getByTestId("home-needs-you").getByRole("heading", { level: 2, name: /^Needs you/ })).toBeVisible();
  await expect(page.locator("main .card")).toHaveCount(0);
});

test("two people are signed in at once, each in their own context", async ({ as }) => {
  const adminPage = await as(admin);
  const lead = await as("platform-lead@example.com");
  await adminPage.reload();
  await expect(adminPage.getByTestId("current-user")).toContainText("(Admin)");
  await expect(lead.getByTestId("current-user")).not.toContainText("(Admin)");
});

test.describe("on the due template", () => {
  test.use({ template: "due" });

  // The due template's history ends 4 days ago, so a 7-day Goal is due on
  // Home with its last Check-in 4 days old.
  test("a lead has a Check-in due, last checked in 4 days ago", async ({ page }) => {
    await signIn(page, "platform-lead@example.com");
    const fourDaysOld = page.getByTestId("home-due-goal").filter({ hasText: "last check-in 4 days ago" });
    await expect(fourDaysOld.first()).toBeVisible();
  });
});
