# Find your role's ID via the Momento CLI:
momento role list
momento role describe -n my-role-name
terraform import momento_custom_role.example r-abcdefg

# Momento custom role management requires a V2 API key (and V2 endpoint).
