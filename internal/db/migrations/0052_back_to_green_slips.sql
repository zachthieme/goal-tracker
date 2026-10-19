-- 0052_back_to_green_slips: a Date Slip can move a Path to Green's
-- back-to-Green date (ticket #242; CONTEXT.md: Date Slip, Path to Green).
--
-- back_to_green marks a slip of the back-to-Green date: its milestone_id is
-- NULL, as a delivery-date slip's is, so the flag is what tells the two apart.
-- Such a slip moves no stored date, since the Check-in that records it already
-- carries the new one. Existing slips moved the delivery date or a Milestone's.

ALTER TABLE date_slips ADD COLUMN back_to_green INTEGER NOT NULL DEFAULT 0;
