module.exports = {
  extends: ["@commitlint/config-conventional"],
  ignores: [
    // Ignore merge commits
    (commit) => commit.includes("Merge branch"),
    (commit) => commit.includes("Merge pull request"),
    // Ignore GitHub bot commits that start with common patterns
    (commit) =>
      commit.startsWith("Update ") || commit.startsWith("Apply suggestion"),
    // Ignore initial commits
    (commit) => commit === "Initial plan" || commit === "Initial commit",
  ],
  rules: {
    "type-enum": [
      2,
      "always",
      [
        "feat",
        "fix",
        "docs",
        "style",
        "refactor",
        "perf",
        "test",
        "build",
        "ci",
        "chore",
        "revert",
      ],
    ],
    "subject-case": [0],
    "header-max-length": [2, "always", 100],
  },
};
