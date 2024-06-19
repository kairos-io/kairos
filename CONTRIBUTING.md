Contributing
============

All contributions are welcome to this project!

How to contribute
-----------------

-  **File an issue** - if you found a bug, want to request an
   enhancement, or want to implement something (bug fix or feature).
-  **Send a pull request** - if you want to contribute code. Please be
   sure to file an issue first.
-  **Check good first-issue** - check out [good first issues](https://github.com/kairos-io/kairos/issues?q=is%3Aopen+is%3Aissue+label%3A%22good+first+issue%22) if you want to contribute to a specific problem
-  **Read about our code of conduct and governance**: Kairos is an Open source, community-driven project with a [governance](https://github.com/kairos-io/kairos/blob/master/GOVERNANCE.md) and adopts CNCF [Code of conduct](https://github.com/kairos-io/kairos/blob/master/CODE_OF_CONDUCT.md)

Guiding user stories
--------------------

These "guiding user stories" summarize the scope and ideals of the Kairos project. These are all stories that can grow with context: every new device or OS that Kairos supports can have an implication for each of these stories. We cannot ask every contribution to satisfy them all at once. Instead, we ask that each new feature or capability should fit into at least one guiding user story and that every lower-numbered user story should be satisfied first.

1. I can install k8s on an experimental device.
2. I can install k8s on an experimental cluster.
3. I can install using a conservative trust model.
4. I can upgrade safely.
5. I can upgrade automatically.
6. I can ensure OS security updates are installed automatically.
7. I can secure my data at rest and in flight.
8. I can install on a production cluster.
9. I can derive from an OS of my choosing.
10. I can install on an edge cluster.

This list is always subject to revision, but notice that each story loses meaning without the ones that come before.

Pull request best practices
---------------------------

We want to accept your pull requests! Please follow these steps:

## Step 1: File an issue

Before writing any code, please file an issue stating the problem you
want to solve or the feature you want to implement. This allows us to
give you feedback before you spend any time writing code. There may be a
known limitation that can't be addressed, or a bug that has already been
fixed in a different way. The issue allows us to communicate and figure
out if it's worth your time to write a bunch of code for the project.
If changes are trivial, use the PR message to write why we need the patch, 
and the motivations behind it.

## Step 2: Fork this repository in GitHub

This will create your own copy of our repository.

## Step 3: Add the upstream source

The upstream source is the project under the Box organization on GitHub.
To add an upstream source for this project, type:

```
git remote add upstream git@github.com:kairos-io/kairos.git
```

This will come in useful later.

## Step 4: Create a feature branch

Create a branch with a descriptive name, such as ``fix/dns``.

## Step 5: Push your feature branch to your fork

1. If working on an issue, signal other contributors that you are actively working on by commenting on the issue.
1. Submit a pull request.
    1. All code PR must be labeled with one of
        - ⚠️ (`:warning:`, major or breaking changes)
        - ✨ (`:sparkles:`, feature additions)
        - 🐛 (`:bug:`, patch and bugfixes)
        - 📖 (`:book:`, documentation or proposals)
        - 🔧 (`:wrenchIcon:`, toolings for developers)
        - :art: ( `:art`, for refactoring )
        - 🌱 (`:seedling:`, minor or other)
        - :penguin: (`:penguin:`, for Distribution or Dockerfile changes)
        - :arrow_up: (`:arrow_up:`, for dependencies bumps)
        - :robot: (`:robot:`, for CI/tests changes )
1. If your PR has multiple commits, you must [squash them into a single commit](https://kubernetes.io/docs/contribute/new-content/open-a-pr/#squashing-commits) before merging your PR.

Individual commits should not be tagged separately, but will generally be
assumed to match the PR. For instance, if you have a bugfix in with
a breaking change, it's generally encouraged to submit the bugfix
separately, but if you must put them in one PR, mark the commit
separately.

All changes must be code reviewed. Expect reviewers to request that you
avoid common [go style mistakes](https://github.com/golang/go/wiki/CodeReviewComments) in your PRs.

As you develop code, continue to push code to your remote feature
branch. Please make sure to include the issue number and the label you're addressing
in your commit message, such as:

```bash
git commit -s -am ":seedling: Drop foo flag (fixes #123)"
```

NOTE: All commits must be signed-off (DCO - Developer Certificate of Origin) so make sure you use the `-s` flag when you commit.

This helps us out by allowing us to track which issue your commit
relates to.

Keep a separate feature branch for each issue you want to address.

## Step 6: Rebase

Before sending a pull request, rebase against upstream, such as:

```bash
git fetch upstream
git rebase upstream/master
```

This will add your changes on top of what's already in upstream,
minimizing merge issues.

## Step 7: Run the tests

Make sure that all tests and lint checks are passing before submitting a pull request.

You can run the lint and test checks locally with:

### Linux
```bash
./earthly.sh +lint
./earthly.sh +test
```

### Windows
```bash
./earthly.ps1 +lint
./earthly.ps1 +test
```

You might want to test your changes with an ISO, to build an ISO locally:

### Linux
```bash
./earthly.sh +iso --FLAVOR=opensuse
```

### Windows
```bash
./earthly.ps1 +iso --FLAVOR=opensuse
```

## Step 8: Send the pull request

Send the pull request from your feature branch to us. Be sure to include
a description that lets us know what work you did.

Keep in mind that we like to see one issue addressed per pull request,
as this helps keep our git history clean and we can more easily track
down issues.

Pull request requirements
-------------------------

### Coding standard

Our pipeline will run some linting to ensure your code additions meet the project standards. If you encounter any issues, please address them before asking for a review from the team. This applies to Go, Yaml and Dockerfile or any newer ones added in the future.

When it comes to code, Kairos components use the Go programming language. In addition to the linting, do your best to write [Effective Go](https://go.dev/doc/effective_go) code. This is the standard that the core team tries to adhere to.

### Testing

All changes in our code, whether new features or bug fixes, must include a test. If you need some help don't hesitate to contact the team through one of our [community channels](https://kairos.io/community/)
