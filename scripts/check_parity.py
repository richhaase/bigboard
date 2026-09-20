#!/usr/bin/env python3
"""Compare the Rust CLI with a built Go reference using isolated Git fixtures.

Usage: python3 scripts/check_parity.py --reference /path/to/go-bigboard --candidate target/debug/bigboard
The fixtures intentionally retain audited Go behavior; this is migration parity,
not evidence that the old analytics policies are correct.
"""
import argparse
import difflib
import json
import os
from pathlib import Path
import subprocess
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--reference', required=True, type=Path)
parser.add_argument('--candidate', default=Path('target/debug/bigboard'), type=Path)
args = parser.parse_args()
reference, candidate = args.reference.resolve(), args.candidate.resolve()
env = dict(os.environ, GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=os.devnull,
           GIT_TERMINAL_PROMPT='0', TZ='UTC')
checks = []


def git(repo, *argv, extra=None):
    return subprocess.check_output(['git', '-C', str(repo), *argv], env=env | (extra or {}), stderr=subprocess.PIPE)


def init(repo):
    repo.mkdir(parents=True)
    git(repo, 'init', '-b', 'main')
    git(repo, 'config', 'user.name', 'Alice Smith')
    git(repo, 'config', 'user.email', 'alice@example.test')
    return repo


def commit(repo, name='Alice Smith', email='alice@example.test', message='change', date='2026-01-15T10:00:00+00:00'):
    git(repo, 'add', '-A')
    git(repo, 'commit', '--allow-empty', '-m', message, extra={
        'GIT_AUTHOR_NAME': name, 'GIT_AUTHOR_EMAIL': email,
        'GIT_COMMITTER_NAME': name, 'GIT_COMMITTER_EMAIL': email,
        'GIT_AUTHOR_DATE': date, 'GIT_COMMITTER_DATE': date})


def write(repo, path, content):
    p = repo / path
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(content)


def check(label, paths, config=None, flags=None, expected_code=0):
    cfg = root / 'config.json'
    cfg.write_text(json.dumps(config or {}))
    command = ['--config', str(cfg), *(flags or ['--export']), *map(str, paths)]
    outputs = [subprocess.run([str(binary), *command], env=env, capture_output=True, text=True, timeout=30)
               for binary in (reference, candidate)]
    a, b = outputs
    assert a.returncode == b.returncode == expected_code, (label, a.returncode, b.returncode, a.stderr, b.stderr)
    if expected_code == 0:
        left, right = json.loads(a.stdout), json.loads(b.stdout)
        if left != right:
            diff = '\n'.join(difflib.unified_diff(json.dumps(left, indent=2, sort_keys=True).splitlines(),
                                                json.dumps(right, indent=2, sort_keys=True).splitlines(),
                                                fromfile='Go', tofile='Rust'))
            raise AssertionError(f'{label}\n{diff}')
        assert bool(a.stderr) == bool(b.stderr), (label, a.stderr, b.stderr)
    else:
        assert not b.stdout, (label, b.stdout)
    checks.append(label)
    print(f'PASS {label}')


with tempfile.TemporaryDirectory(prefix='bigboard-parity-') as tmp:
    root = Path(tmp)
    api = init(root / 'org-a' / 'api')
    write(api, 'source.rs', 'one\ntwo\nthree\n')
    write(api, 'vendor/dependency.rs', 'vendor\n' * 20)
    write(api, 'Cargo.lock', 'lock\n' * 10)
    commit(api)
    write(api, 'source.rs', 'one\nthree\nfour\nfive\n')
    commit(api, 'asmith', message='assisted\n\nCo-authored-by: Claude <noreply@anthropic.com>', date='2026-02-20T08:30:00-07:00')
    write(api, 'readme.md', 'docs\n')
    commit(api, 'Bob', 'bob@example.test', date='2026-02-22T18:30:00+05:30')
    write(api, 'bot.rs', 'bot\n')
    commit(api, 'renovate[bot]', 'renovate[bot]@users.noreply.github.com')
    write(api, 'agent.rs', 'agent\n')
    commit(api, 'Custom Agent', 'agent@team.test')
    write(api, 'employee.rs', 'human\n')
    commit(api, 'Human Employee', 'employee@openai.com')
    git(api, 'mv', 'readme.md', 'renamed.md')
    commit(api, 'Bob', 'bob@example.test', message='rename')
    write(api, 'fuzzy.rs', 'fuzzy\n')
    commit(api, 'Daniel', 'daniel@example.test')
    write(api, 'fuzzy2.rs', 'fuzzy\n')
    commit(api, 'Daniela', 'daniela@example.test')
    for sort in ['total', 'commits', 'added', 'removed', 'net', 'ai', 'impact']:
        check(f'ordinary export sort={sort}', [api], {'sort': sort})
    check('include generated files', [api], {'all_files': True})
    check('explicit AI and bot identities', [api], {'ai_identities': ['@team.test'], 'bot_identities': ['Custom Agent']})
    check('fuzzy identities', [api], {'fuzzy': True})
    check('export ignores since', [api], {'since': '1d'})
    check('config nulls', [api], {'paths': None, 'sort': None, 'fuzzy': None, 'all_files': None})
    check('config paths', [], {'paths': [str(api)]})
    check('named group overrides positionals', [root / 'does-not-exist'], {'groups': {'backend': [str(api)]}}, ['--group', 'backend', '--export'])

    other = init(root / 'org-b' / 'api')
    write(other, 'other.rs', 'other\n')
    commit(other, 'Alice Smith', 'different@example.test')
    check('duplicate basenames and same-name identity', [api, other])
    check('unique display exclusion', [api, other], {'exclude': ['org-a/api']})
    check('basename exclusion', [api, other], {'exclude': ['api']})
    check('glob exclusion', [api, other], {'exclude': ['org-b/*']})
    check('recursive discovery', [root], {'depth': 2})
    symlink = root / 'alias'
    symlink.symlink_to(api, target_is_directory=True)
    check('symlink deduplication', [api, symlink])

    write(api, '.mailmap', 'Canonical Alice <canonical@example.test> Alice Smith <alice@example.test>\nCanonical Alice <canonical@example.test> asmith <alice@example.test>\n')
    check('native mailmap', [api])
    commit(api, message='comment address\n\nCo-authored-by: noreply@anthropic.com (Claude)')
    check('coauthor address comment', [api])
    write(api, 'vendor/bad\tfile.rs', 'quoted\n' * 3)
    commit(api, message='quoted path')
    check('retained quoted-path behavior', [api])
    git(api, 'config', 'log.showRoot', 'false')
    check('retained Git root setting behavior', [api])
    git(api, 'config', '--unset', 'log.showRoot')
    git(api, 'tag', 'main', 'HEAD~2')
    check('retained colliding-tag behavior', [api])
    git(api, 'tag', '-d', 'main')

    shallow = root / 'shallow'
    subprocess.run(['git', 'clone', '--depth=1', api.as_uri(), str(shallow)], env=env, check=True, capture_output=True)
    check('retained shallow-boundary counts', [shallow])
    empty = init(root / 'empty')
    check('empty repository', [empty])
    broken = root / 'broken'; (broken / '.git').mkdir(parents=True)
    check('partial scan failure', [api, broken])
    check('all repositories fail', [broken], expected_code=1)
    check('invalid sort', [api], {'sort': 'nope'}, expected_code=1)
    check('invalid since', [api], {'since': '21d'}, expected_code=1)
    check('invalid theme', [api], {'theme': 'purple'}, expected_code=1)
    check('unknown config field', [api], {'misspelled': True}, expected_code=1)
    check('missing path', [root / 'missing'], expected_code=1)
    check('unknown CLI flag', [], flags=['--since', '1d'], expected_code=2)
    check('no repositories', [root / 'org-a' / 'api' / 'vendor'], expected_code=1)

print(f'All {len(checks)} Go/Rust parity checks passed.')
