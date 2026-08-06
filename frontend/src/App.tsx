import { RouterProvider } from 'react-router-dom';
import { createAppRouter } from '@/app/router';
import ThemeProvider from '@/app/providers/theme';

const router = createAppRouter();

export default function App() {
  return (
    <ThemeProvider>
      <RouterProvider router={router} />
    </ThemeProvider>
  );
}
