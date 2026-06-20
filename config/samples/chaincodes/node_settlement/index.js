"use strict";

const { Contract } = require("fabric-contract-api");

class SettlementContract extends Contract {
  constructor() {
    super("SettlementContract");
  }

  async initLedger(ctx) {
    const settlements = [
      {
        id: "settlement-001",
        debtor: "BankA",
        creditor: "BankB",
        amount: "125000",
        currency: "USD",
        status: "PENDING",
      },
      {
        id: "settlement-002",
        debtor: "BankC",
        creditor: "BankA",
        amount: "73000",
        currency: "EUR",
        status: "PENDING",
      },
    ];

    for (const settlement of settlements) {
      const exists = await this.settlementExists(ctx, settlement.id);
      if (!exists) {
        await ctx.stub.putState(settlement.id, Buffer.from(JSON.stringify(settlement)));
      }
    }

    return settlements;
  }

  async settlementExists(ctx, id) {
    const bytes = await ctx.stub.getState(id);
    return Boolean(bytes && bytes.length > 0);
  }

  async createSettlement(ctx, id, debtor, creditor, amount, currency) {
    this.requireText(id, "id");
    this.requireText(debtor, "debtor");
    this.requireText(creditor, "creditor");
    this.requireText(amount, "amount");
    this.requireText(currency, "currency");

    if (await this.settlementExists(ctx, id)) {
      throw new Error(`Settlement ${id} already exists`);
    }

    const settlement = {
      id,
      debtor,
      creditor,
      amount,
      currency,
      status: "PENDING",
    };

    await ctx.stub.putState(id, Buffer.from(JSON.stringify(settlement)));
    return settlement;
  }

  async readSettlement(ctx, id) {
    const bytes = await ctx.stub.getState(id);
    if (!bytes || bytes.length === 0) {
      throw new Error(`Settlement ${id} does not exist`);
    }

    return JSON.parse(bytes.toString());
  }

  async markSettled(ctx, id) {
    const settlement = await this.readSettlement(ctx, id);
    settlement.status = "SETTLED";

    await ctx.stub.putState(id, Buffer.from(JSON.stringify(settlement)));
    return settlement;
  }

  async getAllSettlements(ctx) {
    const iterator = await ctx.stub.getStateByRange("", "");
    const settlements = [];

    try {
      for (;;) {
        const result = await iterator.next();
        if (result.value && result.value.value) {
          settlements.push(JSON.parse(result.value.value.toString("utf8")));
        }
        if (result.done) {
          return settlements;
        }
      }
    } finally {
      await iterator.close();
    }
  }

  requireText(value, name) {
    if (!value || `${value}`.trim() === "") {
      throw new Error(`${name} is required`);
    }
  }
}

module.exports.contracts = [SettlementContract];
