-- +goose Up

ALTER TABLE score_transfers DROP CONSTRAINT IF EXISTS score_transfers_amount_check;
ALTER TABLE score_transfers DROP CONSTRAINT IF EXISTS score_transfers_amount_nonzero_check;
ALTER TABLE score_transfers ADD CONSTRAINT score_transfers_amount_nonzero_check CHECK (amount <> 0);

-- +goose Down

ALTER TABLE score_transfers DROP CONSTRAINT IF EXISTS score_transfers_amount_nonzero_check;
ALTER TABLE score_transfers ADD CONSTRAINT score_transfers_amount_check CHECK (amount > 0);
