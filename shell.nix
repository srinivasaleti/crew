with import <nixpkgs> { };

mkShell {
  name = "crew";
  packages = [
    go
    gopls
    gnumake
    git
    gh
  ];
}
