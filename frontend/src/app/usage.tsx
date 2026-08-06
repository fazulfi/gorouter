import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Icons } from '@/components/icons';
import { useEffect, useState } from 'react';

export function Usage() {
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setTimeout(() => setLoading(false), 500);
  }, []);

  if (loading) {
    return <div className="flex items-center justify-center py-24">Loading...</div>;
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-3xl font-bold tracking-tight">Usage Analytics</h2>
      </div>
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Tokens</CardTitle>
            <Icons.dollarSign className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">245M</div>
            <p className="text-xs text-muted-foreground">+12% this month</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Requests</CardTitle>
            <Icons.barChart className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">892K</div>
            <p className="text-xs text-muted-foreground">+8% this month</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">PROMPT</CardTitle>
            <Icons.arrowUpRight className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">186M</div>
            <p className="text-xs text-muted-foreground">tokens used</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">COMPLETION</CardTitle>
            <Icons.arrowDownRight className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">59M</div>
            <p className="text-xs text-muted-foreground">tokens used</p>
          </CardContent>
        </Card>
      </div>
      <Card>
        <CardHeader>
          <CardTitle>Usage by Provider</CardTitle>
          <CardDescription>Breakdown of token consumption across providers</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <span className="text-sm">OpenAI</span>
              <span className="text-sm font-medium">48%</span>
            </div>
            <div className="w-full bg-secondary h-2 rounded-full">
              <div className="h-2 rounded-full bg-primary w-[48%]" />
            </div>
            <div className="flex items-center justify-between mt-4">
              <span className="text-sm">Anthropic</span>
              <span className="text-sm font-medium">32%</span>
            </div>
            <div className="w-full bg-secondary h-2 rounded-full">
              <div className="h-2 rounded-full bg-primary w-[32%]" />
            </div>
            <p className="text-sm text-muted-foreground pt-4">Additional provider data will be added here</p>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}