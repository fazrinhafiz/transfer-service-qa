Feature: Fund transfer between accounts
  As a bank customer
  I want to send money to another account
  So that I can pay people quickly, without ever being charged twice

  Background:
    Given Aisyah has an account with a balance of RM 1,000.00
    And Badrul has an account with a balance of RM 20.00

  Scenario: Successful transfer
    When Aisyah transfers RM 125.50 to Badrul
    Then the transfer succeeds
    And Aisyah's balance is RM 874.50
    And Badrul's balance is RM 145.50
    And Aisyah's latest transaction is a DEBIT of RM 125.50
    And Badrul's latest transaction is a CREDIT of RM 125.50

  Scenario: Transfer rejected for insufficient balance
    When Badrul transfers RM 20.01 to Aisyah
    Then the transfer is rejected with "INSUFFICIENT_FUNDS"
    And Badrul's balance is RM 20.00
    And Aisyah's balance is RM 1,000.00
    And Badrul has no transactions

  Scenario: Duplicate request with the same Idempotency-Key is not charged twice
    When Aisyah transfers RM 100.00 to Badrul with Idempotency-Key "rent-october"
    And the same request is sent again with Idempotency-Key "rent-october"
    Then both responses describe the same transfer
    And the second response is marked as a replay
    And Aisyah's balance is RM 900.00
    And Badrul's balance is RM 120.00

  Scenario: Reusing an Idempotency-Key for a different amount is refused
    When Aisyah transfers RM 100.00 to Badrul with Idempotency-Key "groceries"
    And Aisyah transfers RM 250.00 to Badrul with Idempotency-Key "groceries"
    Then the transfer is rejected with "IDEMPOTENCY_KEY_REUSED"
    And Aisyah's balance is RM 900.00

  Scenario Outline: Per-transaction limit of RM 5,000.00
    Given Aisyah has an account with a balance of RM 20,000.00
    When Aisyah transfers RM <amount> to Badrul
    Then the transfer <outcome>

    Examples:
      | amount   | outcome                                            |
      | 4,999.99 | succeeds                                           |
      | 5,000.00 | succeeds                                           |
      | 5,000.01 | is rejected with "PER_TRANSACTION_LIMIT_EXCEEDED" |

  Scenario: Daily limit of RM 10,000.00
    Given Aisyah has an account with a balance of RM 20,000.00
    And Aisyah has already transferred RM 10,000.00 to Badrul today
    When Aisyah transfers RM 0.01 to Badrul
    Then the transfer is rejected with "DAILY_LIMIT_EXCEEDED"
