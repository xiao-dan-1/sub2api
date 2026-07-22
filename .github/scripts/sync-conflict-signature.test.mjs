import assert from 'node:assert/strict'
import { describe, it } from 'node:test'

import {
  classifyConflictIncident,
  conflictPathSignature,
  normalizeConflictPaths
} from './sync-conflict-signature.mjs'

describe('sync conflict path signatures', () => {
  it('normalizes paths with stable sorting, trimming, and deduplication', () => {
    assert.deepEqual(
      normalizeConflictPaths([
        ' frontend/src/api/admin/system.ts ',
        'backend/cmd/server/VERSION',
        '',
        'frontend/src/api/admin/system.ts'
      ]),
      ['backend/cmd/server/VERSION', 'frontend/src/api/admin/system.ts']
    )
  })

  it('sorts paths by deterministic code-unit order', () => {
    assert.deepEqual(normalizeConflictPaths(['a-path', '_path', 'Z-path']), [
      'Z-path',
      '_path',
      'a-path'
    ])
  })

  it('produces the same incident signature for the same path set in a different order', () => {
    const first = conflictPathSignature([
      'backend/internal/config/config_test.go',
      'frontend/src/api/admin/system.ts'
    ])
    const second = conflictPathSignature([
      'frontend/src/api/admin/system.ts',
      'backend/internal/config/config_test.go',
      'frontend/src/api/admin/system.ts'
    ])

    assert.equal(first, second)
    assert.match(first, /^[a-f0-9]{64}$/)
  })

  it('produces a new incident signature when the conflict path set changes', () => {
    const original = conflictPathSignature(['backend/cmd/server/VERSION'])
    const changed = conflictPathSignature([
      'backend/cmd/server/VERSION',
      'backend/internal/handler/admin/system_handler.go'
    ])

    assert.notEqual(original, changed)
  })

  it('classifies first, repeated, and changed conflict incidents', () => {
    const signature = conflictPathSignature(['backend/internal/config/config_test.go'])
    const changed = conflictPathSignature(['frontend/src/api/admin/system.ts'])

    assert.equal(classifyConflictIncident('', signature), 'new')
    assert.equal(classifyConflictIncident(signature, signature), 'repeated')
    assert.equal(classifyConflictIncident(signature, changed), 'changed')
  })
})
