// The harness's smoke test: the seeded app comes up, and the development
// sign-in lands an Admin and a lead on Home.
import { admin, expect, signIn, test } from "./fixtures";

test("an Admin signs in to Home with Risks in the top bar", async ({ page }) => {
  await signIn(page, admin);
  await expect(page).toHaveURL(/\/home$/);
  await expect(page.getByRole("heading", { level: 1, name: "Your week" })).toBeVisible();
  await expect(page.getByTestId("current-user")).toContainText("(Admin)");
  await expect(page.getByRole("link", { name: /^Risks/ })).toBeVisible();
});

test("a lead signs in and sees Your week", async ({ page }) => {
  await signIn(page, "platform-lead@example.com");
  await expect(page.getByRole("heading", { level: 1, name: "Your week" })).toBeVisible();
  await expect(page.getByTestId("home-your-goals")).toBeVisible();
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
    const fourDaysOld = page.getByTestId("home-due-goal").filter({ hasText: "Last check-in 4 days ago" });
    await expect(fourDaysOld.first()).toBeVisible();
  });
});
