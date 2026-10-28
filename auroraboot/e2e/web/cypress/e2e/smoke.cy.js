// Very basic smoke test suite — only exercises endpoints that don't
// require authentication so the CI job can run without wiring the
// auto-generated admin password out of the container logs. Enough to
// catch "did the binary build, start, and serve something sane?"
// regressions, which is the category of bug most likely to slip past
// the Go unit suite.
//
// If/when we need to cover authenticated flows (login, nodes list,
// decommission dialog), start the container with AURORABOOT_ADMIN_PASSWORD
// set in the workflow, plumb it in here as a Cypress env var, and add
// a cy.session()-based login command under cypress/support.

describe("AuroraBoot web smoke", () => {
  // The index.html shell is served for any route the router owns. We
  // don't assert on specific content — just that the server answers.
  // Touching the actual text would make the test brittle to routine UI
  // copy changes that this suite shouldn't gate on.
  it("serves the SPA shell on /", () => {
    cy.request({ url: "/", failOnStatusCode: true }).then((res) => {
      expect(res.status).to.eq(200);
      expect(res.headers["content-type"]).to.match(/text\/html/);
      expect(res.body).to.match(/<!doctype html/i);
    });
  });

  // The SPA router owns unknown paths and the server falls back to
  // index.html so deep links work on reload. The server checks for
  // text/html in the Accept header before serving the SPA shell.
  it("serves the SPA shell on an arbitrary deep link", () => {
    cy.request({
      url: "/nodes",
      failOnStatusCode: true,
      headers: { Accept: "text/html" },
    }).then((res) => {
      expect(res.status).to.eq(200);
      expect(res.body).to.match(/<!doctype html/i);
    });
  });

  // Public install script endpoint — documented entry point for hand-
  // installing the phone-home agent on a node. Failing here means the
  // router shape or the handler's output has drifted.
  it("serves the install-agent script", () => {
    cy.request({ url: "/api/v1/install-agent", failOnStatusCode: true }).then((res) => {
      expect(res.status).to.eq(200);
      expect(res.body).to.include("#!/bin/sh");
      expect(res.body).to.include("AURORABOOT_URL");
      expect(res.body).to.include("kairos-agent-phonehome");
    });
  });

  // Unauthenticated admin endpoints must 401. This catches the class of
  // regression where a route gets accidentally moved out of the
  // admin-auth group.
  it("rejects unauthenticated admin requests with 401", () => {
    cy.request({
      url: "/api/v1/nodes",
      failOnStatusCode: false,
    }).then((res) => {
      expect(res.status).to.eq(401);
    });
  });
});
