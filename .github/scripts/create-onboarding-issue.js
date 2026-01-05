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
 * Create an onboarding issue for a new maintainer
 * @param {Object} github - GitHub API client
 * @param {Object} context - GitHub context
 * @param {string} applicantName - Name of the applicant
 * @param {number} originalIssueNumber - Issue number of the original application
 * @returns {Promise<{issueNumber: number, alreadyExists: boolean, state: string}>}
 */
async function createOnboardingIssue(github, context, applicantName, originalIssueNumber) {
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
  
  // Create onboarding issue body
  const onboardingBody = `🎉 Congratulations! The maintainer application for **${applicantName}** has been approved.

This issue tracks the onboarding tasks that need to be completed.

## Onboarding Checklist

- [ ] Request personal email from applicant (needed for CNCF maintainer registry)
- [ ] Add applicant to Kairos maintainers list: https://github.com/kairos-io/community/blob/main/MAINTAINERS.md
- [ ] Add applicant to [CNCF project maintainers](https://github.com/cncf/foundation/blob/main/project-maintainers.csv)
- [ ] Send an email to cncf-maintainer-changes@cncf.io and CC members@kairos.io and the applicant, requesting the person to be added as maintainer
- [ ] Grant GitHub repository access (per the role mapping in governance; typically GitHub "Maintain")
- [ ] Blog, share on socials and celebrate!

## Reference

- Original application: #${originalIssueNumber}
- See [governance documentation](https://github.com/kairos-io/community/blob/main/GOVERNANCE.md) for role details

---

**Related Issue:** This onboarding issue was automatically created after the maintainer vote was completed in issue #${originalIssueNumber}.`;
  
  // Create the onboarding issue
  const onboardingIssue = await github.rest.issues.create({
    owner: context.repo.owner,
    repo: context.repo.repo,
    title: `Onboarding: ${applicantName}`,
    body: onboardingBody,
    labels: ['onboarding', 'governance']
  });
  
  console.log(`✅ Created onboarding issue #${onboardingIssue.data.number}`);
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
 */
async function commentAndClose(github, context, originalIssueNumber, onboardingIssueNumber, applicantName) {
  // Comment on the original issue
  await github.rest.issues.createComment({
    owner: context.repo.owner,
    repo: context.repo.repo,
    issue_number: originalIssueNumber,
    body: `🎉 The maintainer application for **${applicantName}** has been approved!

An onboarding issue has been created to track the remaining tasks: #${onboardingIssueNumber}

This application issue will now be closed.`
  });
  
  // Close the original issue
  await github.rest.issues.update({
    owner: context.repo.owner,
    repo: context.repo.repo,
    issue_number: originalIssueNumber,
    state: 'closed'
  });
  
  console.log(`✅ Commented on issue #${originalIssueNumber} and closed it`);
  console.log(`✅ Created onboarding issue #${onboardingIssueNumber}`);
}

module.exports = {
  createOnboardingIssue,
  commentAndClose,
  checkExistingOnboardingIssue
};

