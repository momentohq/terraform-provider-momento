# Custom roles can be imported by specifying their ID,
# which you can find via the Momento CLI:
momento role list
terraform import momento_custom_role.example r-abcdefg

# To import by role name instead, use an `import` block with the `identity` argument.
