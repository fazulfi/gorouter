import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';

export function Providers() {
  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-3xl font-bold tracking-tight">Providers</h2>
        <Button>+ Add Provider</Button>
      </div>
      <Card>
        <CardHeader>
          <CardTitle>Manage AI Providers</CardTitle>
          <CardDescription>Configure and monitor your provider integrations</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            <div className="flex items-center justify-between p-4 border rounded-lg">
              <div className="space-y-1">
                <p className="font-medium">OpenAI</p>
                <p className="text-sm text-muted-foreground">Active • 1.2M requests/month</p>
              </div>
              <Button variant="outline">Configure</Button>
            </div>
            <div className="flex items-center justify-between p-4 border rounded-lg">
              <div className="space-y-1">
                <p className="font-medium">Anthropic</p>
                <p className="text-sm text-muted-foreground">Active • 800K requests/month</p>
              </div>
              <Button variant="outline">Configure</Button>
            </div>
            <p className="text-sm text-muted-foreground pt-4">Additional providers will be added here</p>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}