import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Separator } from '@/components/ui/separator';
import { Icons } from '@/components/icons';
import { useTranslations } from '@/i18n';

interface LoginProps {}

export function Login(_props: LoginProps) {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const t = useTranslations();
  const navigate = useNavigate();

  async function onSubmit(event: React.FormEvent) {
    event.preventDefault();
    setIsLoading(true);

    try {
      await simulateLoginSubmit();
    } catch (error) {
      console.error(error);
    } finally {
      setIsLoading(false);
    }
  }

  async function simulateLoginSubmit() {
    return new Promise((resolve) => {
      setTimeout(() => {
        navigate('/dashboard');
        resolve(true);
      }, 500);
    });
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-background p-4">
      <Card className="w-full max-w-md">
        <CardHeader className="space-y-1">
          <Icons.logo className="h-12 w-12 mx-auto" />
          <CardTitle className="text-2xl text-center">Sign in to your account</CardTitle>
          <CardDescription className="text-center">
            Enter your email and password to access your account
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={onSubmit} className="space-y-4">
            <div className="space-y-2">
              <Input
                id="email"
                type="email"
                placeholder="name@example.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
                disabled={isLoading}
              />
              <Input
                id="password"
                type="password"
                placeholder="Password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
                disabled={isLoading}
              />
            </div>
            <Button type="submit" className="w-full" disabled={isLoading}>
              {isLoading && <Icons.spinner className="mr-2 h-4 w-4 animate-spin" />}
              Sign In
            </Button>
            <Separator />
            <div className="grid grid-cols-3 gap-4">
              <Button variant="outline" className="w-full" disabled>
                <Icons.gitHub className="mr-2 h-4 w-4" />
                GitHub
              </Button>
              <Button variant="outline" className="w-full" disabled>
                <Icons.google className="mr-2 h-4 w-4" />
                Google
              </Button>
              <Button variant="outline" className="w-full" disabled>
                <Icons.microsoft className="mr-2 h-4 w-4" />
                Microsoft
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}