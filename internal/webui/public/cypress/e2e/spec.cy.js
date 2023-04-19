describe('Basic Tests for webui', () => {
  beforeEach(() => {
    cy.visit('/')
    cy.intercept({
      method: 'POST',
      url: '/validate',
    }, {log: false}).as('validate')
  })
  it("basic items on the ui exist", () => {
    cy.contains('#cloud-config').should("exist").should("be.visible")
    cy.contains('Welcome to the Installer!').should("exist").should("be.visible")
    cy.contains("p a", "cloud-config config configuration file")
        .should("have.attr", "href", "/local/docs/reference/configuration/")
    cy.get("#cloud-config-help a")
        .should("have.attr", "href", "/local/docs/examples/")
    cy.get("#installation-device").should("have.value", "auto")

    // footer
    cy.get("a .fa-github").should("exist").parent().should("have.attr", "href", "https://github.com/kairos-io/kairos")
    cy.get("a .fa-book").should("exist").parent().should("have.attr", "href", "https://kairos.io/docs")
    cy.get("#reboot-checkbox").should("exist").should("not.be.checked")
    cy.get("#poweroff-checkbox").should("exist").should("not.be.checked")
    cy.get("button").should("exist").invoke("text").should("equal", "Install")
  })
  it('validation works', () => {
    cy.get('.CodeMirror')
        .first()
        .then((editor) => {
          editor[0].CodeMirror.setValue('');
        });

    cy.get(".CodeMirror textarea").type("#cloud-config{enter}users:{enter}  - name: itxaka", {force: true})
    cy.get("#validator-alert").should("have.text", "Valid YAML syntax")

  })
  it('validation fails ', () => {
    cy.get('.CodeMirror')
        .first()
        .then((editor) => {
          editor[0].CodeMirror.setValue('');
        });
    cy.get(".CodeMirror textarea").type("blablabla", {force: true})
    cy.get("#validator-alert").invoke("text").should("match", /Failed validating syntax/)

  })
  it('should install', function () {
    cy.get("button").click()
  });
})