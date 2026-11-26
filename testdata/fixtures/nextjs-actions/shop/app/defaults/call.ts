'use server';

import { validated } from '@/lib/validated';

// Row SA-M1, default_call: a wrapper's result as the default export.
export default validated(async (data) => data.get('id'));
