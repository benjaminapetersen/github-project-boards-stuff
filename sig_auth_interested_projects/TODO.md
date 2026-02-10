
- [Prototype Project](https://github.com/benjaminapetersen/github-project-boards-stuff/blob/main/main.go)

---

- Script that takes:
  - Kubernetes Releave: v1.36
  - List of people: our team, Jordan, whoever
- Script will then:
  - Get all issues and PRs these people have opened, put on our board
  - We will manually sort into projects
  - Then we will manually review these projects and issues
- Basically a `list of epics` that we care about
  - Not individuals and individual updates
  - But when we `review` we will then go `review by projects`
- Mo and Ben own categorize by projects
- Script can `run locally with own creds`
- Standa has one-liner script:
  - `gh search issues --involves '@me' --state open -L1000 --json=url --jq '.[] | .url' | xargs -I {} gh project item-add --owner '@me' 1 --url {}`
    - Just issues for self, but we need PRs and other things also
    - Struggles with pagination, etc.
- Mo says this is our `Go Script`: <https://github.com/kubernetes-sigs/sig-auth-tools/blob/main/main.go>
  - Is Go, can structure anything


---

- Epics: Secret CSI, Secrets Controller, Certificates

  - Each EPIC can have 1 draft issue
  - Just write stuff in the EPIC
  - Somehow link the issues in this?
    - Categorize them by `project` or `epic`, whatever we want to call it
    - SO buckets epic 1,2,3
    - Issues already categorized in there

- Currently have projects

- Currently look at it based on `time` instead of `projects`

  - So `invert` so its not `iteration`
  - `Scrum` not `per person` or `per time`
  - but `scrum` is about `project` regardless of who is working on them
  - so `one place to look at` to tell `everything the team is doing` regardless of `who` across `all projects` that happens to be `right now`

---

1. Our SCRUM board, its useless? how to make it useful?
   1. Can we use `Krossota` to experiment?
   2. [Azure Container Upstream Planning (us)](https://github.com/azure-management-and-platforms/azcu-planning)
      - Generic enough to avoid future change needs
      - Policy, requesting `JIT Admin Access` and other things:
        - [Policy: JIT Admin Access, etc](https://aka.ms/gim/docs/policy/jit)

Give this GitHub Board: https://github.com/orgs/kubernetes/projects/230/views/1 which is the klist of enhancements my team is working on for the current Kubernetes Release, and this GitHub Board: https://github.com/orgs/Azure/projects/821/views/1 which is a list of smaller projects we are watching and maintaining, and this GitHub Board: https://github.com/orgs/Azure/projects/498/views/4 which also contains a variety of additional tasks, I need to fill in this current document which is a prioritization of high level goals as well as our measurements for achieving those goals.  My team is very invested in Auth in Kubernetes, and also the API Machinery of Kubernetes. We also own some sub-projects that ensure best practices, listed on this README https://github.com/kubernetes/community/blob/master/sig-auth/README.md, although many of these are "done" and not actively worked on.  The Secrets Store CSI Driver and Controller are active currently, outside of core Kubernetes.  Our last planning document is here: https://loop.cloud.microsoft/p/eyJ3Ijp7InUiOiJodHRwczovL21pY3Jvc29mdC5zaGFyZXBvaW50LmNvbS8%2FbmF2PWN6MGxNa1ltWkQxaUlYSXlORzFHTVdkZmNtdEhNRXhOYzBremRUZEViWHBwZVMxcmRqZFdRV1JLY2pCUWFIWndaRkZFT0VrNVVUVnZiMVJwZGt4Uk5teEdZalpyZFZaNlF5MG1aajB3TVV0VVRrNVpVbFpNUlRjeVRWcGFXbFpMUmtkTFJVZEtVVkpGV0U1WlJESkVKbU05Sm1ac2RXbGtQVEUlM0QiLCJyIjpmYWxzZX0sInAiOnsidSI6Imh0dHBzOi8vbWljcm9zb2Z0LnNoYXJlcG9pbnQuY29tLzpmbDovdC9BenVyZUNvbnRhaW5lckNvbXB1dGUtVXBzdHJlYW1BdXRoT25lLW9mZk1lZXRpbmdzL0VWM3lwR3BiUGxSS2pVcEJfeUhOM2trQjl2bEFvWkFsd0JOa2l1SmxHSzlKZUE%2FbmF2PWN6MGxNa1owWldGdGN5VXlSa0Y2ZFhKbFEyOXVkR0ZwYm1WeVEyOXRjSFYwWlMxVmNITjBjbVZoYlVGMWRHaFBibVV0YjJabVRXVmxkR2x1WjNNbVpEMWlJWEZXUzJSeFFtd3diREF5ZG1wa1VrZEtVRFZzVVV4d01YRmFjVWxwT1dSSmRqSnJaV2hRVkZoNk1rSmZOMGxmZVhOcGVHdFNObWw2V2xkeVNrNU5TelltWmowd01WWkpSMXBNV1VzMU5rdFRSMVZYV2paTFVrWkpNbE5UUWpjMFVUUXpXRk5LSm1NOUpUSkdKbVpzZFdsa1BURW1lRDBsTjBJbE1qSjNKVEl5SlROQkpUSXlWREJTVkZWSWVIUmhWMDU1WWpOT2RscHVVWFZqTW1ob1kyMVdkMkl5YkhWa1F6VnFZakl4T0ZscFJubE5hbEowVW1wR2JsZ3pTbkpTZWtKTlZGaE9TazB6VlROU1J6RTJZVmhyZEdFeldUTldhMFpyVTI1SmQxVkhhREpqUjFKU1VrUm9TazlXUlRGaU1qbFZZVmhhVFZWVVduTlNiVWt5WVROV1YyVnJUWFJtUkVGNFV6RlNUMVJzYkZOV2EzaEdUbnBLVGxkc2NHRldhM1JIVWpCMFJsSXdjRkpWYTFaWlZHeHNSVTFyVVNVelJDVXlNaVV5UXlVeU1ta2xNaklsTTBFbE1qSTFZelZqT0RSalpDMWhZelk1TFRRd05qY3RZamd5WmkwMk16STNaV013TkRJME16a2xNaklsTjBRJTNEIiwiciI6dHJ1ZX0sImkiOnsiaSI6IjVjNWM4NGNkLWFjNjktNDA2Ny1iODJmLTYzMjdlYzA0MjQzOSJ9fQ%3D%3D.
For this upcoming Trimester, we want to see several things:
1. Downstream AKS
  1. Azure RBAC Improvements
  2. Identity Bindings to public release
  3. Structured AuthN / External IdP to public release
2. Upstream Kubernetes
  1. Workload Identity for Image Pulls to GA
  2. Secrets Sync Controller to Beta and Stability that matches the Secrets Sync CSI Driver
  3. Authentication to Webhooks KEP to Alpha
  4. Constrained Impersonation KEP to Beta
Fill out this document given this set of information.