-- Money is stored as BIGINT sen, never DECIMAL/FLOAT in application code paths.
-- All timestamps are UTC.

CREATE TABLE accounts (
    id          BIGINT       NOT NULL AUTO_INCREMENT,
    owner_name  VARCHAR(100) NOT NULL,
    balance_sen BIGINT       NOT NULL,
    created_at  DATETIME(6)  NOT NULL,
    PRIMARY KEY (id),
    -- Last line of defence: even if application logic were wrong, the
    -- database refuses to store an overdrawn balance.
    CONSTRAINT chk_accounts_balance_non_negative CHECK (balance_sen >= 0)
) ENGINE = InnoDB;

CREATE TABLE transfers (
    id              BIGINT       NOT NULL AUTO_INCREMENT,
    idempotency_key VARCHAR(64)  NOT NULL,
    from_account_id BIGINT       NOT NULL,
    to_account_id   BIGINT       NOT NULL,
    amount_sen      BIGINT       NOT NULL,
    reference       VARCHAR(140) NOT NULL DEFAULT '',
    created_at      DATETIME(6)  NOT NULL,
    PRIMARY KEY (id),
    -- Backstop for idempotency if the Redis key is lost.
    UNIQUE KEY uq_transfers_idempotency_key (idempotency_key),
    -- Serves the daily-limit query: SUM(amount) WHERE from = ? AND created_at >= ?
    KEY idx_transfers_from_created (from_account_id, created_at),
    CONSTRAINT fk_transfers_from FOREIGN KEY (from_account_id) REFERENCES accounts (id),
    CONSTRAINT fk_transfers_to FOREIGN KEY (to_account_id) REFERENCES accounts (id),
    CONSTRAINT chk_transfers_amount_positive CHECK (amount_sen > 0)
) ENGINE = InnoDB;

CREATE TABLE ledger_entries (
    id                      BIGINT                   NOT NULL AUTO_INCREMENT,
    transfer_id             BIGINT                   NOT NULL,
    account_id              BIGINT                   NOT NULL,
    counterparty_account_id BIGINT                   NOT NULL,
    direction               ENUM ('DEBIT', 'CREDIT') NOT NULL,
    amount_sen              BIGINT                   NOT NULL,
    balance_after_sen       BIGINT                   NOT NULL,
    reference               VARCHAR(140)             NOT NULL DEFAULT '',
    created_at              DATETIME(6)              NOT NULL,
    PRIMARY KEY (id),
    KEY idx_ledger_account (account_id, id),
    CONSTRAINT fk_ledger_transfer FOREIGN KEY (transfer_id) REFERENCES transfers (id),
    CONSTRAINT fk_ledger_account FOREIGN KEY (account_id) REFERENCES accounts (id)
) ENGINE = InnoDB;
