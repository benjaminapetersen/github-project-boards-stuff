# CONTEXT

- Always write tests to ensure future changes do not break
  past work
- When making functional changes
  - Do not change function and test in same commit or PR
  - Change function should always pass existing tests
  - Change tests should always work with existing function
  - New functionality should be carefully impelemented
    such to ensure past function not broken
- Security above all
  - We do not leak any information publicly
  - These should aways be private boards