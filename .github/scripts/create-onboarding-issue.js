// Role types supported by the onboarding automation
const ROLE_TYPES = {
  MAINTAINER: 'maintainer',
  CONTRIBUTOR: 'contributor'
};

// Onboarding checklist templates for each role type
const ONBOARDING_TEMPLATES = {
  [ROLE_TYPES.MAINTAINER]: {
    title: (name) => `Onboarding: ${name}`,
    congratsMessage: (name) => `🎉 Congratulations! The maintainer application for **${name}** has been approved.`,
    checklist: `
- [ ] Request personal email from applicant (needed for CNCF maintainer registry)
- [ ] Add applicant to Kairos maintainers list: https://github.com/kairos-io/community/blob/main/MAINTAINERS.md
- [ ] Add applicant to [CNCF project maintainers](https://github.com/cncf/foundation/blob/main/project-maintainers.csv)
- [ ] Send an email to cncf-maintainer-changes@cncf.io and CC members@kairos.io and the applicant, requesting the person to be added as maintainer
- [ ] Add applicant to the [maintainers team](https://github.com/orgs/kairos-io/teams/maintainers)
- [ ] Grant GitHub repository access (per the role mapping in governance; typically GitHub "Maintain")
- [ ] Blog, share on socials and celebrate!`,
    labels: ['onboarding', 'governance'],
    closingComment: (name, issueNumber) => `🎉 The maintainer application for **${name}** has been approved!

An onboarding issue has been created to track the remaining tasks: #${issueNumber}

This application issue will now be closed.`
  },
  [ROLE_TYPES.CONTRIBUTOR]: {
    title: (name) => `Onboarding: ${name}`,
    congratsMessage: (name) => `🎉 Congratulations! The contributor application for **${name}** has been approved.`,
    checklist: `
- [ ] Add applicant to GitHub organization with Triage role
- [ ] Add applicant to the [contributors team](https://github.com/orgs/kairos-io/teams/contributors)
- [ ] Add applicant to contributors list: https://github.com/kairos-io/community/blob/main/CONTRIBUTORS.md
- [ ] Welcome the new contributor in the community chat`,
    labels: ['onboarding', 'governance'],
    closingComment: (name, issueNumber) => `🎉 The contributor application for **${name}** has been approved!

An onboarding issue has been created to track the remaining tasks: #${issueNumber}

This application issue will now be closed.`
  }
};

/**
 * Check if an onboarding issue already exists for an applicant
 * @param {Object} github - GitHub API client
 * @param {Object} context - GitHub context
 * @param {string} applicantName - Name of the applicant
 * @returns {Promise<{exists: boolean, issueNumber: number|null, state: string|null}>}
 */
async function checkExistingOnboardingIssue(github, context, applicantName) {
  // Escape special characters that could affect GitHub search query
  // GitHub search special characters: " \ + - & | ! ( ) { } [ ] ^ ~ * ? :
  const sanitizedApplicantName = applicantName.replace(/["\\]/g, '\\$&');
  
  // Search for existing onboarding issues (both open and closed) with title matching Onboarding: {applicantName}
  const searchQuery = `repo:${context.repo.owner}/${context.repo.repo} is:issue "Onboarding: ${sanitizedApplicantName}" in:title`;
  
  console.log(`🔍 Searching for existing onboarding issues with query: ${searchQuery}`);
  
  const searchResults = await github.rest.search.issuesAndPullRequests({
    q: searchQuery,
    sort: 'created',
    order: 'desc',
    per_page: 10
  });
  
  console.log(`   Found ${searchResults.data.total_count} matching issues`);
  
  // Check if any of the results have the exact title match
  // Note: GitHub search with quotes finds the phrase within the title, but may return partial matches.
  // We need to verify exact title match to avoid false positives.
  for (const issue of searchResults.data.items) {
    const expectedTitle = `Onboarding: ${applicantName}`;
    if (issue.title === expectedTitle) {
      console.log(`   ✅ Found existing onboarding issue #${issue.number} (state: ${issue.state})`);
      return {
        exists: true,
        issueNumber: issue.number,
        state: issue.state
      };
    }
  }
  
  console.log(`   No existing onboarding issue found for "${applicantName}"`);
  return {
    exists: false,
    issueNumber: null,
    state: null
  };
}

/**
 * Create an onboarding issue for a new team member
 * @param {Object} github - GitHub API client
 * @param {Object} context - GitHub context
 * @param {string} applicantName - Name of the applicant
 * @param {number} originalIssueNumber - Issue number of the original application
 * @param {string} roleType - Type of role ('maintainer' or 'contributor')
 * @returns {Promise<{issueNumber: number, alreadyExists: boolean, state: string}>}
 */
async function createOnboardingIssue(github, context, applicantName, originalIssueNumber, roleType = ROLE_TYPES.MAINTAINER) {
  const template = ONBOARDING_TEMPLATES[roleType];
  if (!template) {
    throw new Error(`Unknown role type: ${roleType}. Supported types: ${Object.values(ROLE_TYPES).join(', ')}`);
  }

  // Defense in depth: fetch the latest state of the original issue
  // If it's already closed, processing has already happened (avoids race conditions)
  const originalIssue = await github.rest.issues.get({
    owner: context.repo.owner,
    repo: context.repo.repo,
    issue_number: originalIssueNumber
  });
  
  if (originalIssue.data.state === 'closed') {
    console.log(`⚠️  Original issue #${originalIssueNumber} is already closed, skipping to avoid duplicate processing`);
    // Try to find the existing onboarding issue to return its number
    const existing = await checkExistingOnboardingIssue(github, context, applicantName);
    return {
      issueNumber: existing.issueNumber || 0,
      alreadyExists: true,
      state: existing.state || 'unknown'
    };
  }
  
  // Check if an onboarding issue already exists for this applicant
  const existing = await checkExistingOnboardingIssue(github, context, applicantName);
  
  if (existing.exists) {
    console.log(`⚠️  Onboarding issue already exists for "${applicantName}": #${existing.issueNumber} (${existing.state})`);
    return {
      issueNumber: existing.issueNumber,
      alreadyExists: true,
      state: existing.state
    };
  }
  
  // Create onboarding issue body using the role-specific template
  const onboardingBody = `${template.congratsMessage(applicantName)}

This issue tracks the onboarding tasks that need to be completed.

## Onboarding Checklist
${template.checklist}

## Reference

- Original application: #${originalIssueNumber}
- See [governance documentation](https://github.com/kairos-io/community/blob/main/GOVERNANCE.md) for role details

---

**Related Issue:** This onboarding issue was automatically created after the ${roleType} vote was completed in issue #${originalIssueNumber}.`;
  
  // Create the onboarding issue
  const onboardingIssue = await github.rest.issues.create({
    owner: context.repo.owner,
    repo: context.repo.repo,
    title: template.title(applicantName),
    body: onboardingBody,
    labels: template.labels
  });
  
  console.log(`✅ Created ${roleType} onboarding issue #${onboardingIssue.data.number}`);
  return {
    issueNumber: onboardingIssue.data.number,
    alreadyExists: false,
    state: 'open'
  };
}

/**
 * Comment on the original issue and close it
 * @param {Object} github - GitHub API client
 * @param {Object} context - GitHub context
 * @param {number} originalIssueNumber - Issue number of the original application
 * @param {number} onboardingIssueNumber - Issue number of the created onboarding issue
 * @param {string} applicantName - Name of the applicant
 * @param {string} roleType - Type of role ('maintainer' or 'contributor')
 */
async function commentAndClose(github, context, originalIssueNumber, onboardingIssueNumber, applicantName, roleType = ROLE_TYPES.MAINTAINER) {
  const template = ONBOARDING_TEMPLATES[roleType];
  if (!template) {
    throw new Error(`Unknown role type: ${roleType}. Supported types: ${Object.values(ROLE_TYPES).join(', ')}`);
  }

  // Comment on the original issue
  await github.rest.issues.createComment({
    owner: context.repo.owner,
    repo: context.repo.repo,
    issue_number: originalIssueNumber,
    body: template.closingComment(applicantName, onboardingIssueNumber)
  });
  
  // Close the original issue
  await github.rest.issues.update({
    owner: context.repo.owner,
    repo: context.repo.repo,
    issue_number: originalIssueNumber,
    state: 'closed'
  });
  
  console.log(`✅ Commented on issue #${originalIssueNumber} and closed it`);
  console.log(`✅ Created ${roleType} onboarding issue #${onboardingIssueNumber}`);
}

module.exports = {
  createOnboardingIssue,
  commentAndClose,
  checkExistingOnboardingIssue,
  ROLE_TYPES
};

