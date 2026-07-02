import type { Dispatch, SetStateAction } from 'react';
import { rowCheckboxClassName } from './bulkSelection';

interface BulkDeleteControlsProps {
  total: number;
  selected: Set<string>;
  setSelected: Dispatch<SetStateAction<Set<string>>>;
  ids: string[];
  label: string;
  onDelete: () => void;
}

export function BulkDeleteControls({ total, selected, setSelected, ids, label, onDelete }: BulkDeleteControlsProps) {
  if (total === 0) return null;
  const allSelected = total > 0 && selected.size === total;
  return (
    <div className="bulk-action-bar">
      <label className="bulk-select-all">
        <input
          className={rowCheckboxClassName}
          type="checkbox"
          checked={allSelected}
          onChange={e => setSelected(e.target.checked ? new Set(ids) : new Set())}
        />
        <span>全选</span>
      </label>
      <span className="bulk-selected-count">已选 {selected.size} / {total} 项</span>
      <button className="danger-button compact-button" disabled={selected.size === 0} onClick={onDelete}>批量删除{label}</button>
      {selected.size > 0 && <button className="secondary-button compact-button" onClick={() => setSelected(new Set())}>取消选择</button>}
    </div>
  );
}
