<script lang="ts">
  import { Badge } from '$lib/components/ui/badge';
  import { Button } from '$lib/components/ui/button';
  import { formatDateTimeWIB } from '$lib/utils';

  import {
    auditActionLabel,
    auditPayloadFields,
    formatAuditValue,
    targetTypeLabel,
    type AdminAuditEntry,
  } from '../schemas/audit.schema';

  /**
   * The trail table plus one expanded payload row at a time. The query, the filters,
   * the pagination and the role all belong to AuditLog, which owns them; this
   * component renders, and decides which payload is open.
   *
   * There is no edit and no delete control anywhere: a row is a fact, and the reader
   * is read-only (ADR 0027 §3).
   */
  let { entries }: { entries: AdminAuditEntry[] } = $props();

  // One payload open at a time: two expanded JSON documents is two things to misread.
  let expandedId = $state<number | null>(null);

  function toggle(id: number) {
    expandedId = expandedId === id ? null : id;
  }
</script>

<div class="overflow-x-auto rounded-lg border">
  <table class="w-full min-w-[64rem] text-left text-sm">
    <thead class="bg-muted/60 text-muted-foreground text-xs uppercase">
      <tr>
        <th class="px-3 py-2 font-medium">Waktu (WIB)</th>
        <th class="px-3 py-2 font-medium">Aktor</th>
        <th class="px-3 py-2 font-medium">Aksi</th>
        <th class="px-3 py-2 font-medium">Target</th>
        <th class="px-3 py-2 font-medium">IP</th>
        <th class="px-3 py-2 font-medium">Payload</th>
      </tr>
    </thead>
    <tbody class="divide-y">
      {#each entries as entry (entry.id)}
        {@const fields = auditPayloadFields(entry.payload)}
        <tr>
          <td class="px-3 py-2 whitespace-nowrap">{formatDateTimeWIB(entry.created_at)}</td>
          <td class="px-3 py-2">
            <span class="font-mono text-xs">{entry.admin_email}</span>
          </td>
          <td class="px-3 py-2 whitespace-nowrap">
            <Badge variant="outline" size="sm">{auditActionLabel(entry.action)}</Badge>
            <div class="text-muted-foreground font-mono text-xs">{entry.action}</div>
          </td>
          <td class="px-3 py-2 whitespace-nowrap">
            {#if entry.target_type || entry.target_id}
              <div>{targetTypeLabel(entry.target_type)}</div>
              <div class="text-muted-foreground font-mono text-xs">{entry.target_id}</div>
            {:else}
              <span class="text-muted-foreground text-xs">—</span>
            {/if}
          </td>
          <td class="px-3 py-2 font-mono text-xs whitespace-nowrap">
            {entry.ip || '—'}
          </td>
          <td class="px-3 py-2">
            {#if fields.length > 0}
              <Button
                variant="outline"
                aria-expanded={expandedId === entry.id}
                onclick={() => toggle(entry.id)}
              >
                {expandedId === entry.id ? 'Tutup' : 'Lihat payload'}
              </Button>
            {:else}
              <span class="text-muted-foreground text-xs">Tidak ada payload</span>
            {/if}
          </td>
        </tr>
        {#if expandedId === entry.id}
          <tr>
            <td colspan="6" class="bg-muted/30 p-0">
              <div class="space-y-2 px-4 py-3" data-testid="audit-payload">
                <p class="text-muted-foreground text-xs">
                  Nilai yang dicatat saat aksi ini terjadi. Baris ini tidak bisa diubah.
                </p>
                <dl class="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-2">
                  {#each fields as field (field.key)}
                    <div class="flex flex-wrap items-baseline gap-2">
                      <dt class="text-muted-foreground text-xs font-medium">{field.label}</dt>
                      <dd class="font-mono text-xs">
                        {#if field.change}
                          <span class="text-muted-foreground line-through">
                            {formatAuditValue(field.before)}
                          </span>
                          <span aria-hidden="true">→</span>
                          <span class="font-semibold">{formatAuditValue(field.after)}</span>
                        {:else}
                          {formatAuditValue(field.value)}
                        {/if}
                      </dd>
                    </div>
                  {/each}
                </dl>
              </div>
            </td>
          </tr>
        {/if}
      {/each}
    </tbody>
  </table>
</div>
