DROP TRIGGER IF EXISTS trg_immutable_ledger_entries ON ledger_entries;
DROP FUNCTION IF EXISTS prevent_ledger_tampering;
DROP TABLE IF EXISTS ledger_entries;
