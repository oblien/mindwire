#!/usr/bin/env python3
"""Build, sign and notarize the shared Apple app for direct Mac distribution."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import plistlib
import re
import shutil
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parent.parent
APPLE = ROOT / "apps/apple"
INSTALLER = "Mindwire.dmg"


def run(*args, capture=False):
    result = subprocess.run([str(a) for a in args], cwd=ROOT, check=True,
                            stdout=subprocess.PIPE if capture else None,
                            text=capture)
    return result.stdout if capture else None


def digest(path):
    checksum = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            checksum.update(block)
    return checksum.hexdigest()


def checksum(path):
    path.with_suffix(path.suffix + ".sha256").write_text(f"{digest(path)}  {path.name}\n")


def distribution_profiles():
    profiles = {}
    directory = os.environ.get("MINDWIRE_PROFILES_DIR")
    if directory:
        for path in Path(directory).glob("*.provisionprofile"):
            decoded = run("security", "cms", "-D", "-i", path, capture=True)
            profile = plistlib.loads(decoded.encode())
            entitlements = profile["Entitlements"]
            identifier = entitlements.get("com.apple.application-identifier", "").split(".", 1)[-1]
            if not profile.get("ProvisionsAllDevices") or entitlements.get("get-task-allow"):
                raise ValueError("Use Developer ID distribution profiles, not development or App Store profiles")
            if identifier:
                profiles[identifier] = profile["UUID"]
    return profiles


def build(args):
    if not args.version or not re.fullmatch(r"\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?", args.version):
        raise ValueError("A release version is required")
    if not args.build_number.isdigit() or int(args.build_number) < 1:
        raise ValueError("A positive build number is required")
    sdk = json.loads((ROOT / "packages/sdk/package.json").read_text())["version"]
    daemon = re.search(r'^const Version = "([^"]+)"',
                       (ROOT / "packages/daemon/internal/agent/agent.go").read_text(), re.MULTILINE)
    if sdk != args.version or not daemon or daemon[1] != args.version:
        raise ValueError("Mac app, SDK, daemon and release tag must have the same version")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    archive = output / "Mindwire.xcarchive"
    run("xcodegen", "generate", "--spec", APPLE / "project.yml")
    lock = APPLE / "Mindwire.xcodeproj/project.xcworkspace/xcshareddata/swiftpm/Package.resolved"
    lock.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(APPLE / "Package.resolved", lock)
    command = ["xcodebuild", "-project", APPLE / "Mindwire.xcodeproj", "-scheme", "Mindwire",
               "-configuration", "Release", "-destination", "generic/platform=macOS,variant=Mac Catalyst",
               "-archivePath", archive, "-derivedDataPath", output / "DerivedData",
               "-disableAutomaticPackageResolution", "-onlyUsePackageVersionsFromResolvedFile",
               "-jobs", "4", "ARCHS=arm64 x86_64", "ONLY_ACTIVE_ARCH=NO",
               "ENABLE_HARDENED_RUNTIME=YES", f"MARKETING_VERSION={args.version.split('-')[0]}",
               f"CURRENT_PROJECT_VERSION={args.build_number}"]
    if args.source_packages:
        command += ["-clonedSourcePackagesDirPath", args.source_packages.resolve()]
    if args.archive_only:
        command += ["CODE_SIGNING_ALLOWED=NO"]
    elif os.environ.get("CI"):
        profiles = distribution_profiles()
        required = ["com.oblien.mindwire", "com.oblien.mindwire.notifyservice"]
        if any(identifier not in profiles for identifier in required):
            raise ValueError("Developer ID profiles for Mindwire and its notification extension are required")
        command += ["CODE_SIGN_STYLE=Manual", f"CODE_SIGN_IDENTITY={os.environ['APPLE_IDENTITY']}",
                    f"MINDWIRE_MAC_APP_PROFILE={profiles[required[0]]}",
                    f"MINDWIRE_MAC_EXTENSION_PROFILE={profiles[required[1]]}"]
    else:
        command += ["-allowProvisioningUpdates"]
    command += ["archive"]
    run(*command)
    if args.archive_only:
        return
    export(args, archive)


def export(args, archive):
    output = args.output.resolve()
    identity = os.environ.get("APPLE_IDENTITY", "Developer ID Application")
    profiles = distribution_profiles()
    options = {"method": "developer-id", "destination": "export",
               "teamID": os.environ.get("APPLE_TEAM_ID", "5Q877873WD"),
               "signingStyle": "manual" if profiles else "automatic",
               "signingCertificate": identity, "iCloudContainerEnvironment": "Production"}
    if profiles:
        options["provisioningProfiles"] = profiles
    plist = output / "ExportOptions.plist"
    plist.write_bytes(plistlib.dumps(options))
    if (output / "export").exists():
        shutil.rmtree(output / "export")
    command = ["xcodebuild", "-exportArchive", "-archivePath", archive,
               "-exportPath", output / "export", "-exportOptionsPlist", plist]
    if not os.environ.get("CI"):
        command.append("-allowProvisioningUpdates")
    run(*command)
    app = output / "export/Mindwire.app"
    helper = app / "Contents/Helpers/mindwired"
    # The Go runtime is a nested executable, not an Xcode target. Sign it before
    # sealing the app, with the same hardened runtime and secure timestamp.
    run("codesign", "--force", "--options", "runtime", "--timestamp", "--sign", identity, helper)
    run("codesign", "--force", "--options", "runtime", "--timestamp", "--sign", identity,
        "--preserve-metadata=entitlements,requirements,flags", app)
    run("codesign", "--verify", "--deep", "--strict", app)
    entitlements = plistlib.loads(run("codesign", "-d", "--entitlements", ":-", app, capture=True).encode())
    if (entitlements.get("get-task-allow") or
            entitlements.get("com.apple.developer.icloud-container-environment") != "Production" or
            entitlements.get("com.apple.developer.aps-environment") != "production" or
            entitlements.get("com.apple.security.device.audio-input") is not True or
            entitlements.get("com.apple.security.device.camera") is not True or
            "iCloud.com.oblien.mindwire" not in entitlements.get("com.apple.developer.icloud-container-identifiers", [])):
        raise ValueError("Distribution signing must preserve CloudKit, push, microphone and camera capabilities")
    info = plistlib.loads((app / "Contents/Info.plist").read_bytes())
    executable = app / "Contents/MacOS" / info["CFBundleExecutable"]
    for binary in (executable, helper):
        run("lipo", binary, "-verify_arch", "arm64", "x86_64")
    stage = output / "dmg"
    if stage.exists():
        shutil.rmtree(stage)
    stage.mkdir()
    shutil.copytree(app, stage / "Mindwire.app", symlinks=True)
    applications = stage / "Applications"
    if not applications.is_symlink():
        applications.symlink_to("/Applications")
    installer = output / INSTALLER
    run("hdiutil", "create", "-ov", "-format", "UDZO", "-volname", "Mindwire",
        "-srcfolder", stage, installer)
    run("codesign", "--force", "--timestamp", "--sign", identity, installer)
    run("codesign", "--verify", "--strict", installer)
    checksum(installer)


def credentials(args):
    profile = args.keychain_profile or os.environ.get("APPLE_NOTARY_KEYCHAIN_PROFILE")
    if profile:
        return ["--keychain-profile", profile]
    names = ["APPLE_ID", "APPLE_PASSWORD", "APPLE_TEAM_ID"]
    if any(not os.environ.get(name) for name in names):
        raise ValueError("Apple notarization requires --keychain-profile or APPLE_ID, APPLE_PASSWORD and APPLE_TEAM_ID")
    return ["--apple-id", os.environ["APPLE_ID"], "--password", os.environ["APPLE_PASSWORD"],
            "--team-id", os.environ["APPLE_TEAM_ID"]]


def notary(args):
    path = args.output.resolve() / INSTALLER
    receipt_path = path.with_suffix(".dmg.notary.json")
    commit = run("git", "rev-parse", "HEAD", capture=True).strip()
    if args.command == "submit":
        if receipt_path.exists():
            raise ValueError("A notarization receipt already exists; use finish to resume it")
        # Save the submission before any waiting. Re-running the finish job never
        # rebuilds or re-submits the installer when Apple's service is slow.
        command = ["xcrun", "notarytool", "submit", str(path), *credentials(args), "--output-format", "json"]
        result = subprocess.run(command, capture_output=True, text=True)
        if result.returncode:
            raise RuntimeError("Apple notarization submission failed; check the release credentials")
        submission = json.loads(result.stdout)
        receipt_path.write_text(json.dumps({"id": submission["id"], "sha256": digest(path),
            "commit": commit, "run": os.environ.get("GITHUB_RUN_ID", "local")}, indent=2) + "\n")
        print("Submitted to Apple: " + submission["id"])
        return
    receipt = json.loads(receipt_path.read_text())
    if receipt["sha256"] != digest(path) or receipt["commit"] != commit or receipt["run"] != os.environ.get("GITHUB_RUN_ID", "local"):
        raise ValueError("Saved Apple submission does not match this installer and release run")
    deadline = time.monotonic() + 3600
    while time.monotonic() < deadline:
        result = subprocess.run(["xcrun", "notarytool", "info", receipt["id"], *credentials(args),
                                 "--output-format", "json"], capture_output=True, text=True)
        if result.returncode == 0:
            status = json.loads(result.stdout)["status"]
            print("Apple notarization: " + status, flush=True)
            if status == "Accepted":
                run("xcrun", "stapler", "staple", path)
                run("xcrun", "stapler", "validate", path)
                run("spctl", "--assess", "--type", "open", "--context", "context:primary-signature", path)
                checksum(path)
                return
            if status in ("Invalid", "Rejected"):
                raise RuntimeError("Apple rejected this installer. Submission: " + receipt["id"])
        time.sleep(30)
    raise RuntimeError("Apple is still processing " + receipt["id"] + "; re-run the finish job to resume")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["build", "export", "submit", "finish"])
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--version")
    parser.add_argument("--build-number", default="1")
    parser.add_argument("--source-packages", type=Path)
    parser.add_argument("--keychain-profile", help="Saved notarytool credentials for local notarization")
    parser.add_argument("--archive-only", action="store_true")
    args = parser.parse_args()
    if args.command == "build":
        build(args)
    elif args.command == "export":
        export(args, args.output.resolve() / "Mindwire.xcarchive")
    else:
        notary(args)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        print("Mac release failed: " + str(error), file=sys.stderr)
        sys.exit(1)
