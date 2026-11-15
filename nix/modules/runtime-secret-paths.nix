{ lib }:
settings:
let
  forbidden = [
    "gateway.postgres.password"
    "gateway.master.key"
    "gateway.salt.key"
  ];
  visit =
    prefix: value:
    if builtins.isAttrs value then
      lib.concatLists (
        lib.mapAttrsToList (
          name: nested:
          let
            path = prefix ++ [ name ];
            dotted = lib.concatStringsSep "." path;
          in
          lib.optional (lib.elem (lib.toLower dotted) forbidden) dotted ++ visit path nested
        ) value
      )
    else
      [ ];
in
visit [ ] settings
