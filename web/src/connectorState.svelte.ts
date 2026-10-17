import type { EnrollmentToken } from './types';

export const connectorState = $state<{ selectedId: string; enrollment: EnrollmentToken | null; enrollmentWasReset: boolean }>({
  selectedId: '',
  enrollment: null,
  enrollmentWasReset: false,
});
