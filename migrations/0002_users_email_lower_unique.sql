-- La app normaliza los emails a minúsculas antes de escribir; este índice
-- garantiza la unicidad case-insensitive también ante escrituras que no pasen
-- por esa normalización (cinturón y tirantes sobre el UNIQUE de 0001).
CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));
