import argparse
from pathlib import Path
import re
import subprocess
import tempfile


def version(value):
    if not re.fullmatch(r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)", value):
        raise ValueError("Homebrew requires a stable semantic version.")
    return tuple(map(int, value.split(".")))


def update(tag, archive, cask, generator):
    if not tag.startswith("v"):
        raise ValueError("Expected a stable release tag.")
    requested = tag[1:]
    target = version(requested)
    if cask.exists():
        previous = re.search(r'^\s*version\s+"([^"]+)"\s*$', cask.read_text(), re.MULTILINE)
        if previous is None:
            raise ValueError("Existing cask has no recognizable version; refusing to replace it.")
        if version(previous.group(1)) > target:
            print(f"Keeping newer Homebrew cask {previous.group(1)}; release {tag} will not downgrade it.")
            return
    content = subprocess.check_output(["bash", str(generator), requested, str(archive)])
    subprocess.run(["ruby", "-c"], input=content, check=True)
    cask.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(dir=cask.parent, delete=False) as output:
        temporary = Path(output.name)
        output.write(content)
    try:
        temporary.replace(cask)
    finally:
        temporary.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("tag")
    parser.add_argument("archive", type=Path)
    parser.add_argument("cask", type=Path)
    parser.add_argument("generator", type=Path)
    args = parser.parse_args()
    try:
        update(args.tag, args.archive, args.cask, args.generator)
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        raise SystemExit(f"Cannot update Homebrew cask: {error}") from error


if __name__ == "__main__":
    main()
