import type { Dispatch, SetStateAction } from 'react';

export const rowCheckboxClassName = 'row-select-checkbox';

export function toggleSelected(setSelected: Dispatch<SetStateAction<Set<string>>>, id: string) {
  setSelected(prev => {
    const next = new Set(prev);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    return next;
  });
}
