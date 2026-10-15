// Allure 3 report configuration: `npx allure@3.19.1 generate` picks this up.
export default {
  name: "Transfer Service QA",
  output: "./allure-report",
  plugins: {
    awesome: {
      options: {
        reportName: "Transfer Service QA",
        // Group by business capability rather than by test file.
        groupBy: ["epic", "feature", "story"],
      },
    },
  },
};
