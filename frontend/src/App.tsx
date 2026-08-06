import { RouterProvider } from 'react-router-dom';
import { createAppRouter } from '@/app/router';

const router = createAppRouter();

export default function App() {
  return <RouterProvider router={router} />;
}
