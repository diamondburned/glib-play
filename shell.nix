{ pkgs ? import <nixpkgs> {} }:

pkgs.mkShell {
	buildInputs = with pkgs; [
		(go.overrideAttrs(old: {
			version = "1.17beta1";
			src = builtins.fetchurl {
				url    = "https://golang.org/dl/go1.17rc1.linux-arm64.tar.gz";
				sha256 = "sha256:0kps5kw9yymxawf57ps9xivqrkx2p60bpmkisahr8jl1rqkf963l";
			};
			doCheck = false;
		}))

		clang
		gnome.gdk-pixbuf
		gnome.glib
		pkg-config
	];

	CGO_ENABLED = "1";
}
