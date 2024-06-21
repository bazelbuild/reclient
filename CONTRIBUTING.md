# How to contribute

## Before you begin

### Sign our Contributor License Agreement

Contributions to this project must be accompanied by a
[Contributor License Agreement](https://cla.developers.google.com/about) (CLA).
You (or your employer) retain the copyright to your contribution; this simply
gives us permission to use and redistribute your contributions as part of the
project.

Visit <https://cla.developers.google.com/> to see your current agreements or to
sign a new one.

### Review our community guidelines

This project follows
[Google's Open Source Community Guidelines](https://opensource.google/conduct/).

## Contribution process

1.  Create a pull request with your changes against the
    [main](https://github.com/bazelbuild/reclient/commits/main/) branch.
1.  Assign a reviewer to your pull request and get the pull request approved.

    The reviewer can be the person who has most recently edited the file. If
    unsure, please assign gkousik@ / ramymedhat@ as reviewers.

1.  Wait for the pull request to be imported into the source-of-truth repository
    internally. The GitHub Pull Request will remain open until it is merged
    internally. Once its merged internally, change will appear on the external
    GitHub repository, with the author metadata preserved. Your Pull Request
    will subsequently be closed.

    As of now, there's no SLA around this process, but feel free to follow up on
    the pull request if you see that it has not been merged for 2 weeks after it
    has been approved.

### Contribution Policy

We welcome changes that fall into one of these categories

1.  Bug fixes
1.  Dependency updates
1.  Reduction of duplication and removal of dead code
1.  Tech debt reduction - during Reclient development we accumulated dozens of
    tech debt tasks, which while useful, were never implemented. The new
    contributions will be cross-checked against those tasks and accepted if they
    lead to tech debt reduction.

### Rejection criteria

1.  The contributions that don't fall into one of these categories MAY be
    rejected.
1.  The changes that don't fall into one of these categories AND are specific to
    inhouse solutions of external customers are LIKELY to be rejected.
1.  The changes that don't meet the acceptance criteria OR increase operational
    load by making software less maintainable WILL be rejected

### Code reviews

All submissions, including submissions by project members, require review. We
use GitHub pull requests for this purpose. Consult
[GitHub Help](https://help.github.com/articles/about-pull-requests/) for more
information on using pull requests.
