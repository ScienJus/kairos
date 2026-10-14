#!/usr/bin/env python3
"""Verify an isolated stopped-Core SQLite + managed-Artifact recovery set."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import sqlite3
import subprocess
import tempfile
import time
import urllib.request
import uuid


def verify(binary, directory):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    processes = []

    def start(database, artifacts):
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0))
            port = listener.getsockname()[1]
        address = '127.0.0.1:' + str(port)
        env = {key: value for key, value in os.environ.items() if not key.startswith('KAIROS_')}
        env.update(KAIROS_AUTH_MODE='trusted', KAIROS_POSTGRES_DSN='', KAIROS_LISTEN_ADDR=address,
                   KAIROS_SQLITE_PATH=str(database), KAIROS_ARTIFACT_DIR=str(artifacts))
        log = open(directory / ('core-' + str(port) + '.log'), 'wb')
        process = subprocess.Popen([str(binary)], env=env, stdout=log, stderr=log)
        processes.append((process, log))
        base = 'http://' + address
        for _ in range(100):
            try:
                with opener.open(base + '/healthz', timeout=1) as response:
                    if response.status == 200:
                        return process, base
            except OSError:
                pass
            if process.poll() is not None:
                raise RuntimeError('Fixture Core exited; inspect its private log')
            time.sleep(.1)
        raise RuntimeError('Fixture Core did not become healthy')

    def stop(process):
        process.terminate()
        process.wait(timeout=15)
        if process.returncode != 0:
            raise RuntimeError('Core did not stop cleanly')

    def api(base, route, value=None):
        headers = {'X-Kairos-Actor-Id': 'restore-proof', 'X-Kairos-Actor-Kind': 'human',
                   'Content-Type': 'application/json', 'Idempotency-Key': str(uuid.uuid4())}
        request = urllib.request.Request(base + '/api/v1' + route, headers=headers,
                                        data=None if value is None else json.dumps(value).encode())
        with opener.open(request, timeout=5) as response:
            return json.load(response)['data']

    try:
        source = directory / 'source'
        source.mkdir(mode=0o700)
        process, base = start(source / 'core.db', source / 'artifacts')
        api(base, '/definitions/blackboards/restore-proof/versions', {'name': 'Restore proof', 'suggested_tags': []})
        work = api(base, '/work-items', {'definition_id': 'restore-proof', 'mode': 'blackboard',
                                       'title': 'Restore a durable result', 'goal': 'Keep data and content together', 'tags': []})
        task = api(base, '/work-items/' + work['id'] + '/tasks',
                   {'title': 'Publish proof', 'executor': 'human', 'allowed_roles': [], 'tags': []})
        claim = api(base, '/tasks/' + task['id'] + '/claims', {})
        content = b'isolated-backup-proof-v1\n'
        boundary = 'fixture-' + uuid.uuid4().hex
        body = (f'--{boundary}\r\nContent-Disposition: form-data; name="claim_id"\r\n\r\n{claim["id"]}\r\n'
                f'--{boundary}\r\nContent-Disposition: form-data; name="name"\r\n\r\nproof\r\n'
                f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="proof.txt"\r\n'
                'Content-Type: application/octet-stream\r\n\r\n').encode() + content + f'\r\n--{boundary}--\r\n'.encode()
        request = urllib.request.Request(base + '/api/v1/tasks/' + task['id'] + '/artifact-uploads', data=body,
                                         headers={'X-Kairos-Actor-Id': 'restore-proof', 'X-Kairos-Actor-Kind': 'human',
                                                  'Idempotency-Key': str(uuid.uuid4()), 'Content-Type': 'multipart/form-data; boundary=' + boundary})
        with opener.open(request, timeout=5) as response:
            artifact = json.load(response)['data']
        api(base, '/tasks/' + task['id'] + '/submissions', {'claim_id': claim['id'], 'result': 'Proof is durable', 'artifact_ids': [artifact['id']]})
        api(base, '/work-items/' + work['id'] + '/completion', {'result': 'Ready to verify restoration'})
        stop(process)
        # Copy the full stopped database set, including sidecars if present, plus
        # its matching managed store. No active service is copied or overwritten.
        backup = directory / 'backup'
        shutil.copytree(source, backup)
        restored = directory / 'restored'
        shutil.copytree(backup, restored)
        with sqlite3.connect(str(restored / 'core.db')) as connection:
            integrity = connection.execute('PRAGMA quick_check').fetchone()[0]
        if integrity != 'ok':
            raise RuntimeError('Restored SQLite integrity check failed')
        restored_process, restored_base = start(restored / 'core.db', restored / 'artifacts')
        view = api(restored_base, '/work-items/' + work['id'] + '/context')
        if view['work_item']['status'] != 'completed' or view['work_item']['result'] != 'Ready to verify restoration':
            raise RuntimeError('Restored WorkItem differs')
        if len(view['artifacts']) != 1 or view['artifacts'][0]['id'] != artifact['id']:
            raise RuntimeError('Restored Artifact provenance differs')
        request = urllib.request.Request(restored_base + '/api/v1/artifacts/' + artifact['id'] + '/content',
                                         headers={'X-Kairos-Actor-Id': 'restore-proof', 'X-Kairos-Actor-Kind': 'human'})
        with opener.open(request, timeout=5) as response:
            restored_content = response.read()
        if restored_content != content:
            raise RuntimeError('Restored Artifact bytes differ')
        stop(restored_process)
        return {'scope': 'isolated stopped-Core SQLite and local managed Artifact store',
                'platform': os.uname().sysname, 'sqlite_integrity': integrity,
                'work_item_status': view['work_item']['status'], 'artifact_id_preserved': True,
                'content_sha256': hashlib.sha256(restored_content).hexdigest(),
                'database_and_artifact_store_restored_together': True,
                'not_verified': ['PostgreSQL', 'online backups', 'systemd', 'Kubernetes', 'cross-version upgrades']}
    finally:
        for process, log in reversed(processes):
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
            log.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--server', type=Path, default=Path(__file__).resolve().parents[2] / 'bin/kairos-server')
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    if not args.server.is_file():
        parser.error('Build kairos-server first or pass --server')
    with tempfile.TemporaryDirectory(prefix='kairos-restore-proof-') as temporary:
        result = verify(args.server.resolve(), Path(temporary))
    encoded = json.dumps(result, indent=2) + '\n'
    if args.output:
        args.output.write_text(encoded)
    print(encoded, end='')


if __name__ == '__main__':
    main()
