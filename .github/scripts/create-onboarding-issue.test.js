const { test } = require('node:test');
const assert = require('node:assert/strict');

const {
  createOnboardingIssue,
  checkExistingOnboardingIssue,
  ROLE_TYPES
} = require('./create-onboarding-issue.js');

const context = { repo: { owner: 'kairos-io', repo: 'community' } };

function mockGithub({ issues = [], originalState = 'open' } = {}) {
  const created = [];
  let nextNumber = 1000;
  const github = {
    rest: {
      search: {
        issuesAndPullRequests: async ({ q }) => {
          const match = q.match(/"((?:[^"\\]|\\.)*)" in:title/);
          const phrase = match ? match[1].replace(/\\(.)/g, '$1') : '';
          const items = issues.filter((issue) => issue.title.includes(phrase));
          return { data: { total_count: items.length, items } };
        }
      },
      issues: {
        get: async ({ issue_number }) => ({ data: { number: issue_number, state: originalState } }),
        create: async (params) => {
          const issue = { number: nextNumber++, title: params.title, state: 'open' };
          created.push(params);
          issues.push(issue);
          return { data: issue };
        }
      }
    }
  };
  return { github, created };
}

test('maintainer vote creates its own issue when a contributor onboarding exists', async () => {
  const { github, created } = mockGithub({
    issues: [{ number: 10, title: 'Onboarding: Jane Doe', state: 'closed' }]
  });

  const result = await createOnboardingIssue(github, context, 'Jane Doe', 42, ROLE_TYPES.MAINTAINER);

  assert.equal(result.alreadyExists, false);
  assert.equal(created.length, 1);
  assert.notEqual(created[0].title, 'Onboarding: Jane Doe');
});

test('contributor vote creates its own issue when a maintainer onboarding exists', async () => {
  const { github, created } = mockGithub({
    issues: [{ number: 11, title: 'Onboarding (maintainer): Jane Doe', state: 'open' }]
  });

  const result = await createOnboardingIssue(github, context, 'Jane Doe', 42, ROLE_TYPES.CONTRIBUTOR);

  assert.equal(result.alreadyExists, false);
  assert.equal(created.length, 1);
  assert.equal(created[0].title, 'Onboarding: Jane Doe');
});

for (const roleType of Object.values(ROLE_TYPES)) {
  test(`same-role duplicate is detected for ${roleType}`, async () => {
    const { github, created } = mockGithub();

    const first = await createOnboardingIssue(github, context, 'Jane Doe', 42, roleType);
    const second = await createOnboardingIssue(github, context, 'Jane Doe', 43, roleType);

    assert.equal(first.alreadyExists, false);
    assert.equal(second.alreadyExists, true);
    assert.equal(second.issueNumber, first.issueNumber);
    assert.equal(created.length, 1);
  });
}

test('already-closed original returns the onboarding issue of the same role', async () => {
  const issues = [
    { number: 10, title: 'Onboarding: Jane Doe', state: 'closed' },
    { number: 20, title: 'Onboarding (maintainer): Jane Doe', state: 'open' }
  ];

  const maintainer = await createOnboardingIssue(
    mockGithub({ issues: [...issues], originalState: 'closed' }).github,
    context, 'Jane Doe', 42, ROLE_TYPES.MAINTAINER
  );
  assert.equal(maintainer.alreadyExists, true);
  assert.equal(maintainer.issueNumber, 20);

  const contributor = await createOnboardingIssue(
    mockGithub({ issues: [...issues], originalState: 'closed' }).github,
    context, 'Jane Doe', 42, ROLE_TYPES.CONTRIBUTOR
  );
  assert.equal(contributor.alreadyExists, true);
  assert.equal(contributor.issueNumber, 10);
});

test('checkExistingOnboardingIssue matches only the requested role', async () => {
  const { github } = mockGithub({
    issues: [{ number: 10, title: 'Onboarding: Jane Doe', state: 'closed' }]
  });

  const asContributor = await checkExistingOnboardingIssue(github, context, 'Jane Doe', ROLE_TYPES.CONTRIBUTOR);
  assert.equal(asContributor.exists, true);
  assert.equal(asContributor.issueNumber, 10);

  const asMaintainer = await checkExistingOnboardingIssue(github, context, 'Jane Doe', ROLE_TYPES.MAINTAINER);
  assert.equal(asMaintainer.exists, false);
});
