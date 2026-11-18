// Cypress configuration for the AuroraBoot web smoke suite.
//
// CI starts the server on localhost:8080 (see the test-ui-builder job in
// .github/workflows/tests.yml); override with CYPRESS_BASE_URL for local
// runs against a different address.
const { defineConfig } = require("cypress");

module.exports = defineConfig({
  e2e: {
    baseUrl: process.env.CYPRESS_BASE_URL || "http://localhost:8080",
    // CI-friendly defaults: let the UI take a moment to mount, don't record
    // videos (they clutter the artifact upload and we keep screenshots
    // on failure for post-mortem).
    defaultCommandTimeout: 10000,
    pageLoadTimeout: 30000,
    video: false,
    screenshotOnRunFailure: true,
    retries: { runMode: 2, openMode: 0 },
  },
});
