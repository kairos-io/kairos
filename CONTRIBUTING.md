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

Pull request best practices
---------------------------

We want to accept your pull requests. Please follow these steps:

## Step 1: File an issue

Before writing any code, please file an issue stating the problem you
want to solve or the feature you want to implement. This allows us to
give you feedback before you spend any time writing code. There may be a
known limitation that can't be addressed, or a bug that has already been
fixed in a different way. The issue allows us to communicate and figure
out if it's worth your time to write a bunch of code for the project.

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

As you develop code, continue to push code to your remote feature
branch. Please make sure to include the issue number you're addressing
in your commit message, such as:

```bash
git commit -am "Drop foo flag (fixes #123)"
```

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

```bash
./earthly.sh +lint
./earthly.sh +test
```

## Step 8: Send the pull request

Send the pull request from your feature branch to us. Be sure to include
a description that lets us know what work you did.

Keep in mind that we like to see one issue addressed per pull request,
as this helps keep our git history clean and we can more easily track
down issues.
