-- Keep OpenClaw workspace keys on one managed billing group.
-- The group rate is an initial tariff; later administrator changes are preserved
-- by runtime reconciliation.
UPDATE groups
SET name = 'CLAW',
    rate_multiplier = 0.4,
    updated_at = NOW()
WHERE name IN ('Claw Hub I · OpenClaw', 'CLAW')
  AND deleted_at IS NULL
  AND (name = 'CLAW' OR NOT EXISTS (
      SELECT 1 FROM groups existing_claw
      WHERE existing_claw.name = 'CLAW'
        AND existing_claw.deleted_at IS NULL
  ));

